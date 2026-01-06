package ton

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-storage-provider/pkg/transport"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-storage-provider/pkg/contract"
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

func NewService(ctx context.Context, seedPhrase string, internalDBPath string, downloadsPath string) (*Service, error) {
	storage.Logger = func(v ...any) {}

	cfg, err := config.LoadConfig(internalDBPath)
	if err != nil {
		_, privKey, _ := ed25519.GenerateKey(nil)
		cfg = &config.Config{
			Key:		privKey,
			ListenAddr:	"0.0.0.0:14321",
			DownloadsPath:	downloadsPath,
		}
	}
	cfg.DownloadsPath = downloadsPath

	lsCfg, err := liteclient.GetConfigFromUrl(ctx, "https://ton.org/global.config.json")
	if err != nil {
		return nil, fmt.Errorf("failed to get ton config: %w", err)
	}

	lc := liteclient.NewConnectionPool()
	if err := lc.AddConnectionsFromConfig(ctx, lsCfg); err != nil {
		return nil, fmt.Errorf("failed to add connections: %w", err)
	}

	api := ton.NewAPIClient(lc, ton.ProofCheckPolicyFast).WithRetry()

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
	if err := gateway.StartClient(); err != nil {
		return nil, fmt.Errorf("failed to start adnl gateway: %w", err)
	}

	dhtGateway := adnl.NewGateway(dhtKey)
	if err := dhtGateway.StartClient(); err != nil {
		return nil, fmt.Errorf("failed to start dht gateway: %w", err)
	}

	dhtClient, err := dht.NewClientFromConfig(dhtGateway, lsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to init dht: %w", err)
	}

	storageServer := storage.NewServer(dhtClient, gateway, cfg.Key, false, 12)
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
		api:		api,
		wallet:		w,
		storage:	store,
		connector:	connector,
		providerClient:	provClient,
		dht:		dhtClient,
		config:		cfg,
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

	if err := torrent.Start(false, true, false); err != nil {
		return nil, fmt.Errorf("failed to start torrent seeding: %w", err)
	}

	if err := s.storage.SetTorrent(torrent); err != nil {
		return nil, fmt.Errorf("failed to save torrent to db: %w", err)
	}

	return torrent.BagID, nil
}

func (s *Service) HireProvider(ctx context.Context, bagID []byte, providerAddrStr string, amount tlb.Coins) (string, error) {

	provAddr, err := parseAddressAny(providerAddrStr)
	if err != nil {
		return "", err
	}

	rates, err := s.providerClient.FetchProviderRates(ctx, bagID, provAddr.Data())
	if err != nil {
		return "", fmt.Errorf("failed to fetch rates: %w", err)
	}

	if !rates.Available {
		return "", fmt.Errorf("provider is not accepting requests")
	}

	offer := provider.CalculateBestProviderOffer(rates)

	newProviderData := provider.NewProviderData{
		Address:	provAddr,
		MaxSpan:	offer.Span,
		PricePerMBDay:	tlb.FromNanoTON(offer.RatePerMBNano),
	}

	providersList := []provider.NewProviderData{newProviderData}

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
				Address:	address.NewAddress(0, 0, p.Key),
				MaxSpan:	p.MaxSpan,
				PricePerMBDay:	p.RatePerMB,
			})
		}
	}

	contractAddr, body, stateInit, err := s.providerClient.BuildAddProviderTransaction(ctx, bagID, s.wallet.Address(), providersList)
	if err != nil {
		return "", fmt.Errorf("failed to build tx: %w", err)
	}

	bodyCells, err := cell.FromBOC(body)
	if err != nil {
		return "", fmt.Errorf("failed to parse body boc: %w", err)
	}

	var stateInitStruct *tlb.StateInit
	if len(stateInit) > 0 {
		siCells, err := cell.FromBOC(stateInit)
		if err != nil {
			return "", fmt.Errorf("failed to parse stateInit boc: %w", err)
		}
		stateInitStruct = &tlb.StateInit{}
		if err := tlb.LoadFromCell(stateInitStruct, siCells.BeginParse()); err != nil {
			return "", fmt.Errorf("failed to load stateInit: %w", err)
		}
	}

	msg := &wallet.Message{
		Mode:	1,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled:	true,
			Bounce:		true,
			DstAddr:	contractAddr,
			Amount:		amount,
			Body:		bodyCells,
			StateInit:	stateInitStruct,
		},
	}

	log.Printf("Sending tx to Storage Contract %s...", contractAddr.String())

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

		downloadDir := filepath.Join(s.config.DownloadsPath, bagHex)
		os.MkdirAll(downloadDir, 0755)

		tor = storage.NewTorrent(downloadDir, s.storage, s.connector)
		tor.BagID = bagID

		if err := tor.Start(true, true, false); err != nil {
			return fmt.Errorf("failed to start torrent: %w", err)
		}
		if err := s.storage.SetTorrent(tor); err != nil {
			return fmt.Errorf("failed to save torrent to db: %w", err)
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
	if len(addrStr) == 64 {
		b, err := hex.DecodeString(addrStr)
		if err != nil {
			return nil, fmt.Errorf("invalid hex: %w", err)
		}
		return address.NewAddress(0, 0, b), nil
	}
	return address.ParseAddr(addrStr)
}

func (s *Service) WaitForFile(ctx context.Context, bagID []byte, filename string) (string, error) {
	bagHex := hex.EncodeToString(bagID)

	targetPath := filepath.Join(s.config.DownloadsPath, bagHex, filename)

	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return "", fmt.Errorf("timeout waiting for file download: %s", filename)
		case <-ticker.C:

			if _, err := os.Stat(targetPath); err == nil {
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
