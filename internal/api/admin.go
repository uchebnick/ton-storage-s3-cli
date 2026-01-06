package api

import (
	"encoding/hex"
	"log"
	"strconv"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/xssnick/tonutils-go/tlb"
)

type AdminServer struct {
	app	*fiber.App
	db	*database.DB
	tonSvc	*ton.Service
}

func NewAdminServer(db *database.DB, tonSvc *ton.Service) *AdminServer {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(logger.New())
	app.Use(cors.New())

	s := &AdminServer{
		app:	app,
		db:	db,
		tonSvc:	tonSvc,
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

	v1.Post("/files/:id/restore", s.restoreFile)
	v1.Post("/files/:id/replicate", s.manualReplicate)

	v1.Get("/contracts/:id/audit", s.auditContract)
	v1.Post("/contracts/:id/withdraw", s.withdrawContract)
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
		"file":		file,
		"contracts":	contracts,
	})
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

	go func() {
		if err := s.tonSvc.DownloadBag(c.Context(), bagBytes); err != nil {
			log.Printf("Background restore error for file %d: %v", id, err)
		}
	}()

	return c.JSON(fiber.Map{"status": "restore_started", "bag_id": file.BagID})
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
		FileID:		f.ID,
		ProviderAddr:	newProvider,
		ContractAddr:	contractAddr,
		BalanceNano:	amount.Nano().Int64(),
		Status:		"active",
	}
	s.db.RegisterContract(c.Context(), newC)

	return c.JSON(fiber.Map{
		"status":	"hired",
		"provider":	newProvider,
		"contract":	contractAddr,
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

	txHash, err := s.tonSvc.WithdrawFunds(c.Context(), contr.BagID, contr.ProviderAddr)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "success", "tx_hash": txHash})
}
