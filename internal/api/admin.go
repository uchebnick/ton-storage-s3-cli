package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/bits"
	"os"
	"path/filepath"
	"strconv"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/xssnick/tonutils-go/tlb"
)

type AdminServer struct {
	app    *fiber.App
	db     *database.DB
	tonSvc *ton.Service
}

func NewAdminServer(db *database.DB, tonSvc *ton.Service) *AdminServer {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             500 * 1024 * 1024,
	})

	app.Use(logger.New())
	app.Use(cors.New())

	app.Static("/", "./web")

	s := &AdminServer{
		app:    app,
		db:     db,
		tonSvc: tonSvc,
	}

	s.registerRoutes()
	return s
}

func (s *AdminServer) Start(addr string) error {
	log.Printf("🕹️  Admin API running on %s", addr)
	return s.app.Listen(addr)
}

func (s *AdminServer) registerRoutes() {
	v1 := s.app.Group("/api/v1")

	v1.Get("/files", s.listFiles)
	v1.Get("/files/:id", s.getFileDetails)
	v1.Get("/bags", s.getBagsStats)

	v1.Post("/upload", s.uploadFile)
	v1.Get("/files/:id/download", s.downloadFile)
	v1.Delete("/files/:id", s.deleteFile)
	v1.Post("/files/:id/restore", s.restoreFile)
	v1.Post("/files/:id/replicate", s.manualReplicate)
	v1.Get("/files/:id/stats", s.getFileStats)

	v1.Get("/contracts/:id/audit", s.auditContract)
	v1.Post("/contracts/:id/withdraw", s.withdrawContract)
}

func (s *AdminServer) getBagsStats(c *fiber.Ctx) error {
	stats, err := s.tonSvc.GetAllBagsFullStatus()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"bags": stats})
}

func (s *AdminServer) listFiles(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 50)
	offset := c.QueryInt("offset", 0)

	files, err := s.db.ListFiles(c.Context(), limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(files)
}

func (s *AdminServer) getFileDetails(c *fiber.Ctx) error {
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	file, err := s.db.GetFileByID(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "File not found"})
	}

	contracts, err := s.db.GetFileContracts(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to load contracts"})
	}

	return c.JSON(fiber.Map{
		"file":      file,
		"contracts": contracts,
	})
}

func (s *AdminServer) uploadFile(c *fiber.Ctx) error {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "File is required"})
	}

	bucket := c.FormValue("bucket", "default")
	replicas, _ := strconv.Atoi(c.FormValue("replicas", "3"))

	if exists, _ := s.db.BucketExists(c.Context(), bucket); !exists {
		s.db.CreateBucket(c.Context(), bucket)
	}

	uploadDir := "./var/downloads"
	os.MkdirAll(uploadDir, 0755)

	localPath := filepath.Join(uploadDir, fileHeader.Filename)
	if err := c.SaveFile(fileHeader, localPath); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to save file: " + err.Error()})
	}

	absPath, _ := filepath.Abs(localPath)
	bagIDBytes, err := s.tonSvc.CreateBag(c.Context(), absPath)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "TON CreateBag failed: " + err.Error()})
	}
	bagIDHex := hex.EncodeToString(bagIDBytes)

	newFile := &database.File{
		BucketName:     bucket,
		ObjectKey:      fileHeader.Filename,
		BagID:          bagIDHex,
		SizeBytes:      fileHeader.Size,
		TargetReplicas: replicas,
		Status:         "pending",
	}

	id, err := s.db.CreateFile(c.Context(), newFile)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB Insert failed: " + err.Error()})
	}

	return c.Status(201).JSON(fiber.Map{
		"id":     id,
		"bag_id": bagIDHex,
		"status": "created",
		"path":   localPath,
	})
}

func (s *AdminServer) downloadFile(c *fiber.Ctx) error {
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	file, err := s.db.GetFileByID(c.Context(), id)
	if err != nil {
		return c.Status(404).SendString("File not found in DB")
	}

	bagBytes, _ := hex.DecodeString(file.BagID)

	filePath, err := s.tonSvc.GetPathToBagFile(bagBytes, file.ObjectKey)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": "File missing on server disk. Use /restore endpoint first.",
		})
	}

	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, file.ObjectKey))
	return c.SendFile(filePath)
}

func (s *AdminServer) restoreFile(c *fiber.Ctx) error {
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	file, err := s.db.GetFileByID(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "File not found"})
	}

	bagBytes, err := hex.DecodeString(file.BagID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Invalid BagID in DB"})
	}

	jobID, err := s.db.StartDownloadJob(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB Error: " + err.Error()})
	}

	go func() {
		ctx := context.Background()
		log.Printf("📥 [Job %d] Restore started for %s", jobID, file.ObjectKey)
		
		if err := s.tonSvc.DownloadBag(ctx, bagBytes); err != nil {
			log.Printf("❌ Restore init failed: %v", err)
			s.db.FinishDownloadJob(c.Context(), jobID, false, err.Error())
			return
		}

		_, err := s.tonSvc.WaitForFile(ctx, bagBytes, file.ObjectKey)
		
		if err == nil {
			log.Printf("✅ [Job %d] Restore success: %s", jobID, file.ObjectKey)
			s.db.FinishDownloadJob(ctx, jobID, true, "")
		} else {
			log.Printf("⚠️ [Job %d] Restore failed (timeout): %v", jobID, err)
			s.db.FinishDownloadJob(ctx, jobID, false, err.Error())
		}
	}()

	return c.JSON(fiber.Map{
		"status":  "restore_started",
		"job_id":  jobID,
		"message": "Downloading from TON network...",
		"bag_id":  file.BagID,
	})
}

func (s *AdminServer) manualReplicate(c *fiber.Ctx) error {
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	f, err := s.db.GetFileByID(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "File not found"})
	}

	contracts, _ := s.db.GetFileContracts(c.Context(), id)
	excludes := make([]string, 0)
	for _, contr := range contracts {
		excludes = append(excludes, contr.ProviderAddr)
	}

	newProvider, err := s.tonSvc.FindRandomProvider(c.Context(), excludes)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "No providers found: " + err.Error()})
	}

	bagBytes, _ := hex.DecodeString(f.BagID)
	amount := tlb.MustFromTON("0.2")

	contractAddr, err := s.tonSvc.HireProvider(c.Context(), bagBytes, newProvider, amount)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Hire failed: " + err.Error()})
	}

	newC := &database.Contract{
		FileID:       f.ID,
		ProviderAddr: newProvider,
		ContractAddr: contractAddr,
		BalanceNano:  amount.Nano().Int64(),
		Status:       "pending", // ИСПРАВЛЕНО: Pending (ждем проверки аудитора)
	}
	s.db.RegisterContract(c.Context(), newC)

	return c.JSON(fiber.Map{
		"status": "hired_pending", 
		"provider": newProvider, 
		"contract": contractAddr,
	})
}

func (s *AdminServer) auditContract(c *fiber.Ctx) error {
	cid, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	contr, err := s.db.GetContractByID(c.Context(), cid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Contract not found"})
	}

	report, err := s.tonSvc.AuditProvider(c.Context(), contr.BagID, contr.ProviderAddr)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(report)
}

func (s *AdminServer) withdrawContract(c *fiber.Ctx) error {
	cid, _ := strconv.ParseInt(c.Params("id"), 10, 64)

	contr, err := s.db.GetContractByID(c.Context(), cid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Contract not found"})
	}

	txHash, err := s.tonSvc.RemoveProvider(c.Context(), contr.BagID, contr.ProviderAddr)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	s.db.MarkContractFailed(c.Context(), cid)

	return c.JSON(fiber.Map{"status": "removed", "tx_hash": txHash})
}

func (s *AdminServer) getFileStats(c *fiber.Ctx) error {
	id, _ := strconv.ParseInt(c.Params("id"), 10, 64)
	file, err := s.db.GetFileByID(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Not found"})
	}

	bagBytes, _ := hex.DecodeString(file.BagID)
	
	speed, total, err := s.tonSvc.GetTorrentStats(bagBytes)
	
	tor := s.tonSvc.GetStorage().GetTorrent(bagBytes)
	peers := 0
	downloaded := uint64(0)
	completed := false
	active := false

	if tor != nil {
		peers = len(tor.GetPeers())
		active, _ = tor.IsActive()
		
		if tor.Info != nil {
			mask := tor.PiecesMask()
			piecesCount := 0
			for _, b := range mask {
				piecesCount += bits.OnesCount8(b)
			}
			
			downloaded = uint64(piecesCount) * uint64(tor.Info.PieceSize)
			
			fileSizeUint := uint64(file.SizeBytes)

			if downloaded > fileSizeUint { 
				downloaded = fileSizeUint 
			}
			
			if downloaded == fileSizeUint { 
				completed = true 
			}
		}
	}

	return c.JSON(fiber.Map{
		"upload_speed":   speed,
		"uploaded_total": total,
		"file_size":      file.SizeBytes,
		"peers":          peers,
		"active":         active,
		"downloaded":     downloaded,
		"completed":      completed,
	})
}

func (s *AdminServer) deleteFile(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid ID"})
	}

	file, err := s.db.GetFileByID(c.Context(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "File not found in DB"})
	}

	bagBytes, err := hex.DecodeString(file.BagID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Invalid BagID hex"})
	}

	if err := s.tonSvc.DeleteLocalFile(bagBytes); err != nil {
		log.Printf("⚠️ Failed to delete local file %s: %v", file.BagID, err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to delete files: " + err.Error()})
	}

	log.Printf("🗑️ File %s (ID: %d) deleted via API", file.BagID, id)

	return c.JSON(fiber.Map{
		"status":  "deleted_locally",
		"message": "Local files removed. Torrent removed from memory. Use /restore to download again.",
		"bag_id":  file.BagID,
	})
}