package ton

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"math/bits"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"errors"
	"bytes"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/adnl"
	adnlAddress "github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-storage-provider/pkg/contract"
	"github.com/xssnick/tonutils-storage-provider/pkg/transport"
	"github.com/xssnick/tonutils-storage/config"
	"github.com/xssnick/tonutils-storage/db"
	"github.com/xssnick/tonutils-storage/provider"
	"github.com/xssnick/tonutils-storage/storage"
)

type Service struct {
	api		ton.APIClientWrapped
	wallet		*wallet.Wallet
	storage		*db.Storage
	connector	storage.NetConnector
	providerClient	*provider.Client
	dht		*dht.Client
	config		*config.Config
}

func NewService(ctx context.Context, seedPhrase string, internalDBPath string, downloadsPath string, publicIP string) (*Service, error) {
	storage.Logger = log.Println

	cfg, err := config.LoadConfig(internalDBPath)
	if err != nil {
		_, privKey, _ := ed25519.GenerateKey(nil)
		cfg = &config.Config{
			Key:           privKey,
			ListenAddr:    "0.0.0.0:14321",
			DownloadsPath: downloadsPath,
		}
	}
	cfg.DownloadsPath = downloadsPath
	
	if publicIP != "" {
		cfg.ExternalIP = publicIP
	}

	lsCfg, err := liteclient.GetConfigFromUrl(ctx, "https://ton.org/global.config.json")
	if err != nil {
		return nil, fmt.Errorf("failed to get ton config: %w", err)
	}

	lc := liteclient.NewConnectionPool()
	if err := lc.AddConnectionsFromConfig(ctx, lsCfg); err != nil {
		return nil, fmt.Errorf("failed to add connections: %w", err)
	}

	api := ton.NewAPIClient(lc, ton.ProofCheckPolicyFast).WithRetry().WithTimeout(60 * time.Second)

	words := strings.Split(seedPhrase, " ")
	w, err := wallet.FromSeed(api, words, wallet.V4R2)
	if err != nil {
		return nil, fmt.Errorf("failed to init wallet: %w", err)
	}
	log.Printf("Wallet initialized: %s", w.Address().String())

	_, dhtKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}


	gateway := adnl.NewGateway(cfg.Key)
	
	if cfg.ExternalIP != "" {
		ip := net.ParseIP(cfg.ExternalIP)
		if ip == nil {
			return nil, fmt.Errorf("invalid external IP in config: %s", cfg.ExternalIP)
		}

		gateway.SetAddressList([]*adnlAddress.UDP{
			{
				IP:   ip,
				Port: 14321,
			},
		})

		if err := gateway.StartServer(cfg.ListenAddr, 12); err != nil {
			return nil, fmt.Errorf("failed to start adnl server: %w", err)
		}
		log.Printf("🚀 ADNL Gateway started in SERVER mode on %s (Ext: %s)", cfg.ListenAddr, cfg.ExternalIP)
	} else {
		log.Println("⚠️ ExternalIP not set. Starting in Client mode (Downloads only, No Uploads!)")
		if err := gateway.StartClient(); err != nil {
			return nil, fmt.Errorf("failed to start adnl gateway: %w", err)
		}
	}

	dhtGateway := adnl.NewGateway(dhtKey)
	if err := dhtGateway.StartClient(); err != nil {
		return nil, fmt.Errorf("failed to start dht gateway: %w", err)
	}

	dhtClient, err := dht.NewClientFromConfig(dhtGateway, lsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to init dht: %w", err)
	}


	isServerMode := cfg.ExternalIP != ""
	storageServer := storage.NewServer(dhtClient, gateway, cfg.Key, isServerMode, 12)
	connector := storage.NewConnector(storageServer)

	os.MkdirAll(filepath.Join(internalDBPath, "leveldb"), 0755)
	ldb, err := leveldb.OpenFile(filepath.Join(internalDBPath, "leveldb"), &opt.Options{
		WriteBuffer: 64 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open leveldb: %w", err)
	}

	store, err := db.NewStorage(ldb, connector, 0, true, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to init storage db: %w", err)
	}
	storageServer.SetStorage(store)

	transp := transport.NewClient(dhtGateway, dhtClient)
	provClient := provider.NewClient(store, api, transp)

	return &Service{
		api:            api,
		wallet:         w,
		storage:        store,
		connector:      connector,
		providerClient: provClient,
		dht:            dhtClient,
		config:         cfg,
	}, nil
}

func (s *Service) CreateBag(ctx context.Context, localPath string) ([]byte, error) {
	localPath = filepath.Clean(localPath)

	rootPath, dirName, files, err := s.storage.DetectFileRefs(localPath)
	if err != nil {
		return nil, fmt.Errorf("detect files failed: %w", err)
	}

	desc := filepath.Base(localPath)
	torrent, err := storage.CreateTorrent(ctx, rootPath, dirName, desc, s.storage, s.connector, files, nil)
	if err != nil {
		return nil, fmt.Errorf("create torrent failed: %w", err)
	}

	if err := torrent.Start(true, true, false); err != nil {
		return nil, fmt.Errorf("failed to start torrent seeding: %w", err)
	}

	if err := s.storage.SetTorrent(torrent); err != nil {
		return nil, fmt.Errorf("failed to save torrent to db: %w", err)
	}

	return torrent.BagID, nil
}

type BagFullStatus struct {
	BagID         string `json:"bag_id"`
	Peers         int    `json:"peers"`
	UploadSpeed   uint64 `json:"upload_speed"`
	DownloadSpeed uint64 `json:"download_speed"`
	UploadedTotal uint64 `json:"uploaded_total"`
	Downloaded    uint64 `json:"downloaded"`
	FileSize      uint64 `json:"file_size"`
	Completed     bool   `json:"completed"`
	ActiveDownload bool   `json:"active_download"`
	ActiveUpload   bool   `json:"active_upload"`
	HeaderLoaded  bool   `json:"header_loaded"`
}

// GetAllBagsFullStatus собирает подробную статистику по всем торрентам в ноде
func (s *Service) GetAllBagsFullStatus() ([]BagFullStatus, error) {
	torrents := s.storage.GetAll()
	result := make([]BagFullStatus, 0, len(torrents))

	for _, t := range torrents {
		var dow, upl uint64
		peers := t.GetPeers()
		for _, p := range peers {
			dow += p.GetDownloadSpeed()
			upl += p.GetUploadSpeed()
		}

		activeDownload, activeUpload := t.IsActive()
		
		var downloaded uint64
		completed := false
		if t.Info != nil {
			mask := t.PiecesMask()
			downloadedPieces := 0
			for _, b := range mask {
				downloadedPieces += bits.OnesCount8(b)
			}
			downloaded = uint64(downloadedPieces * int(t.Info.PieceSize))
			if downloaded > t.Info.FileSize {
				downloaded = t.Info.FileSize
			}
			completed = uint32(downloadedPieces) == t.Info.PiecesNum()
		}

		result = append(result, BagFullStatus{
			BagID:         hex.EncodeToString(t.BagID),
			Peers:         len(peers),
			UploadSpeed:   upl,
			DownloadSpeed: dow,
			UploadedTotal: t.GetUploadStats(),
			Downloaded:    downloaded,
			FileSize:      t.Info.FileSize,
			Completed:     completed,
			ActiveDownload:        activeDownload,
			ActiveUpload:          activeUpload,
			HeaderLoaded:  t.Header != nil,
		})
	}

	return result, nil
}



func (s *Service) HireProvider(ctx context.Context, bagID []byte, providerAddrStr string, amount tlb.Coins) (string, error) {
	// --- ИСПРАВЛЕНИЕ АДРЕСА ---
	var provAddr *address.Address
	var err error

	// 1. Сначала пробуем стандартный парсер (для EQ... или 0:...)
	provAddr, err = address.ParseAddr(providerAddrStr)
	if err != nil {
		// 2. Если не вышло, пробуем как Raw Hex (то, что шлет Replicator)
		if len(providerAddrStr) == 64 {
			decoded, decodeErr := hex.DecodeString(providerAddrStr)
			if decodeErr == nil {
				// Провайдеры обычно в воркчейне 0
				provAddr = address.NewAddress(0, 0, decoded)
				err = nil 
			}
		}
	}

	// Если все равно ошибка — возвращаем её
	if err != nil || provAddr == nil {
		return "", fmt.Errorf("invalid provider address: %w", err)
	}
	// ---------------------------

	// 2. Получение тарифов
	rates, err := s.providerClient.FetchProviderRates(ctx, bagID, provAddr.Data())
	if err != nil {
		return "", fmt.Errorf("failed to fetch rates: %w", err)
	}

	if !rates.Available {
		return "", fmt.Errorf("provider is not accepting requests")
	}

	// 3. Расчет предложения
	offer := provider.CalculateBestProviderOffer(rates)

	newProviderData := provider.NewProviderData{
		Address:       provAddr,
		MaxSpan:       offer.Span,
		PricePerMBDay: tlb.FromNanoTON(offer.RatePerMBNano),
	}

	providersList := []provider.NewProviderData{newProviderData}

	// 4. Получение текущего списка (чтобы не затереть других)
	contractData, err := s.providerClient.FetchProviderContract(ctx, bagID, s.wallet.Address())
	if err != nil {
		if !errors.Is(err, contract.ErrNotDeployed) {
			return "", fmt.Errorf("failed to fetch contract info: %w", err)
		}
	} else {
		for _, p := range contractData.Providers {
			if bytes.Equal(p.Key, provAddr.Data()) {
				continue
			}
			providersList = append(providersList, provider.NewProviderData{
				Address:       address.NewAddress(0, 0, p.Key),
				MaxSpan:       p.MaxSpan,
				PricePerMBDay: p.RatePerMB,
			})
		}
	}

	// 5. Построение транзакции
	contractAddr, body, stateInit, err := s.providerClient.BuildAddProviderTransaction(ctx, bagID, s.wallet.Address(), providersList)
	if err != nil {
		return "", fmt.Errorf("failed to build tx: %w", err)
	}

	// 6. Парсинг Body
	bodyCell, err := cell.FromBOC(body)
	if err != nil {
		return "", fmt.Errorf("failed to parse body boc: %w", err)
	}

	// 7. Парсинг StateInit
	var stateInitStruct *tlb.StateInit
	if len(stateInit) > 0 {
		siCell, err := cell.FromBOC(stateInit)
		if err != nil {
			return "", fmt.Errorf("failed to parse stateInit boc: %w", err)
		}
		
		stateInitStruct = &tlb.StateInit{}
		if err := tlb.LoadFromCell(stateInitStruct, siCell.BeginParse()); err != nil {
			return "", fmt.Errorf("failed to load stateInit: %w", err)
		}
	}



	msg := &wallet.Message{
		Mode: 1, 
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			DstAddr:     contractAddr,
			Amount:      amount, 
			Body:        bodyCell,    
			StateInit:   stateInitStruct,
		},
	}

	log.Printf("Sending tx to Storage Contract %s (Sent: %s)...", contractAddr.String(), amount.String())

	_, _, err = s.wallet.SendManyWaitTransaction(ctx, []*wallet.Message{msg})
	if err != nil {
		return "", fmt.Errorf("tx failed: %w", err)
	}

	return contractAddr.String(), nil
}

func (s *Service) DownloadBag(ctx context.Context, bagID []byte) error {
	bagHex := hex.EncodeToString(bagID)

	tor := s.storage.GetTorrent(bagID)

	if tor == nil {

		savePath := filepath.Join(s.config.DownloadsPath, bagHex)
		
		if err := os.MkdirAll(savePath, 0755); err != nil {
			return fmt.Errorf("failed to create dir: %w", err)
		}

		tor = storage.NewTorrent(savePath, s.storage, s.connector)
		tor.BagID = bagID

		if err := tor.Start(true, true, false); err != nil {
			return fmt.Errorf("failed to start new torrent: %w", err)
		}

		if err := s.storage.SetTorrent(tor); err != nil {
			return fmt.Errorf("failed to set torrent to storage: %w", err)
		}
	} else {

		if err := tor.Start(true, true, false); err != nil {
			return fmt.Errorf("failed to restart torrent: %w", err)
		}
	}

	return nil
}

func (s *Service) CheckHealth(ctx context.Context, bagID []byte, providerAddrStr string) (bool, error) {
	provAddr, err := parseAddressAny(providerAddrStr)
	if err != nil {
		return false, err
	}

	info, err := s.providerClient.RequestProviderStorageInfo(ctx, bagID, provAddr.Data(), s.wallet.Address())
	if err != nil {
		return false, fmt.Errorf("request info failed: %w", err)
	}

	return info.Status == "active", nil
}

func parseAddressAny(addrStr string) (*address.Address, error) {
	b, err := hex.DecodeString(addrStr)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	return address.NewAddress(0, 0, b), nil

	
}

func (s *Service) WaitForFile(ctx context.Context, bagID []byte, filename string) (string, error) {
	bagHex := hex.EncodeToString(bagID)
	
	targetPath := filepath.Join(s.config.DownloadsPath, bagHex, filename)

	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return "", fmt.Errorf("timeout waiting for file download: %s", filename)
		case <-ticker.C:
			info, err := os.Stat(targetPath)
			if err == nil && info.Size() > 0 {
				return targetPath, nil
			}
		}
	}
}

func (s *Service) GetPathToBagFile(bagID []byte, filename string) (string, error) {
	bagHex := hex.EncodeToString(bagID)

	targetPath := filepath.Join(s.config.DownloadsPath, bagHex, filename)
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath, nil
	}

	flatPath := filepath.Join(s.config.DownloadsPath, filename)
	if _, err := os.Stat(flatPath); err == nil {
		return flatPath, nil
	}

	return "", os.ErrNotExist
}

func (s *Service) GetTorrentStats(bagID []byte) (uploadSpeed uint64, uploadedTotal uint64, err error) {
	tor := s.storage.GetTorrent(bagID)
	if tor == nil {
		return 0, 0, fmt.Errorf("torrent not found")
	}

	peers := tor.GetPeers()
	for _, p := range peers {
		uploadSpeed += p.GetUploadSpeed()
	}
	
	return uploadSpeed, tor.GetUploadStats(), nil
}

func (s *Service) StartSeeding(ctx context.Context) error {
	torrents := s.storage.GetAll()
	
	count := 0
	for _, t := range torrents {
		if err := t.Start(true, true, false); err != nil {
			log.Printf("⚠️ Failed to resume seeding for bag %s: %v", hex.EncodeToString(t.BagID), err)
			continue
		}
		count++
	}
	
	log.Printf("🚀 Resumed seeding for %d files from database", count)
	return nil
}

func (s *Service) PingProvider(ctx context.Context, bagID []byte, providerKeyHex string) error {
	provKey, err := hex.DecodeString(providerKeyHex)
	if err != nil {
		return fmt.Errorf("invalid provider key hex: %w", err)
	}

	info, err := s.providerClient.RequestProviderStorageInfo(ctx, bagID, provKey, s.wallet.Address())
	if err != nil {
		return err
	}

	log.Printf("Provider Status: %s", info.Status)

	return nil
}

func (s *Service) DeleteLocalFile(bagID []byte) error {
	tor := s.storage.GetTorrent(bagID)
	if tor == nil {
		return nil
	}

	err := s.storage.RemoveTorrent(tor, true)
	if err != nil {
		return fmt.Errorf("failed to remove torrent: %w", err)
	}

	return nil
}