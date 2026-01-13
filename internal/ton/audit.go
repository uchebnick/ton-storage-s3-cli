package ton

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-storage/provider"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"

)

type ProviderReport struct {
	IsHealthy     bool
	Status        string
	FailureReason string
	LastProofAt   time.Time
	Balance       *big.Int
}

func (s *Service) AuditProvider(ctx context.Context, bagIdStr, providerAddrStr string) (*ProviderReport, error) {
	bag, err := hex.DecodeString(bagIdStr)
	if err != nil {
		return nil, fmt.Errorf("invalid bag id: %w", err)
	}

	var provAddr *address.Address
	if len(providerAddrStr) == 64 {
		b, _ := hex.DecodeString(providerAddrStr)
		provAddr = address.NewAddress(0, 0, b)
	} else {
		provAddr, err = address.ParseAddr(providerAddrStr)
		if err != nil {
			return nil, fmt.Errorf("invalid provider address: %w", err)
		}
	}

	tor := s.storage.GetTorrent(bag)
	if tor == nil {
		return &ProviderReport{
			IsHealthy:     false,
			Status:        "local_missing",
			FailureReason: "Torrent not loaded in memory",
		}, nil
	}

	peers := tor.GetPeers()
	providerKeyHex := hex.EncodeToString(provAddr.Data())

	for _, p := range peers {
		if p.Addr == providerKeyHex {
			return &ProviderReport{
				IsHealthy:   true,
				Status:      "connected",
				LastProofAt: time.Now(), 
			}, nil
		}
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	info, err := s.providerClient.RequestProviderStorageInfo(pingCtx, bag, provAddr.Data(), s.wallet.Address())
	
	if err != nil {
		return &ProviderReport{
			IsHealthy:     false,
			Status:        "unreachable",
			FailureReason: fmt.Sprintf("Ping failed: %v", err),
		}, nil
	}

	isHealthy := (info.Status == "active")
	
	if info.Status == "resolving" || info.Status == "untrusted" {
		isHealthy = true 
	}

	return &ProviderReport{
		IsHealthy:     isHealthy,
		Status:        info.Status,
		FailureReason: info.Reason,
		LastProofAt:   time.Now(),
	}, nil
}

func (s *Service) RemoveProvider(ctx context.Context, bagIdStr, providerAddrStr string) (string, error) {
	bag, err := hex.DecodeString(bagIdStr)
	if err != nil {
		return "", fmt.Errorf("invalid bag id: %w", err)
	}

	var targetProvAddr *address.Address
	if len(providerAddrStr) == 64 {
		b, _ := hex.DecodeString(providerAddrStr)
		targetProvAddr = address.NewAddress(0, 0, b)
	} else {
		targetProvAddr, err = address.ParseAddr(providerAddrStr)
		if err != nil {
			return "", fmt.Errorf("invalid provider address: %w", err)
		}
	}

	contractData, err := s.providerClient.FetchProviderContract(ctx, bag, s.wallet.Address())
	if err != nil {
		return "", fmt.Errorf("failed to fetch contract data: %w", err)
	}

	var remainingProviders []provider.NewProviderData
	targetKeyHex := hex.EncodeToString(targetProvAddr.Data())
	found := false

	for _, p := range contractData.Providers {
		if hex.EncodeToString(p.Key) == targetKeyHex {
			found = true
			continue 
		}

		remainingProviders = append(remainingProviders, provider.NewProviderData{
			Address:       address.NewAddress(0, 0, p.Key),
			MaxSpan:       p.MaxSpan,
			PricePerMBDay: p.RatePerMB,
		})
	}

	if !found {
		return "", fmt.Errorf("provider %s is not in the contract list (already removed?)", providerAddrStr)
	}

	if len(remainingProviders) == 0 {
		return s.WithdrawAllFunds(ctx, bag)
	}

	contractAddr, bodyBytes, _, err := s.providerClient.BuildAddProviderTransaction(
		ctx, bag, s.wallet.Address(), remainingProviders,
	)
	if err != nil {
		return "", fmt.Errorf("failed to build update transaction: %w", err)
	}

	bodyCell, err := cell.FromBOC(bodyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse body BOC: %w", err)
	}

	msg := &wallet.Message{
		Mode: 1, 
		InternalMessage: &tlb.InternalMessage{
			Bounce:  true,
			DstAddr: contractAddr,
			Amount:  tlb.MustFromTON("0.05"), 
			Body:    bodyCell,
		},
	}

	tx, _, err := s.wallet.SendManyWaitTransaction(ctx, []*wallet.Message{msg})
	if err != nil {
		return "", fmt.Errorf("transaction failed: %w", err)
	}

	return hex.EncodeToString(tx.Hash), nil
}

func (s *Service) WithdrawAllFunds(ctx context.Context, bag []byte) (string, error) {
	contractAddr, bodyBytes, err := s.providerClient.BuildWithdrawalTransaction(bag, s.wallet.Address())
	if err != nil {
		return "", err
	}
	
	bodyCell, err := cell.FromBOC(bodyBytes)
	if err != nil {
		return "", err
	}

	msg := &wallet.Message{
		Mode: 1,
		InternalMessage: &tlb.InternalMessage{
			Bounce:  true,
			DstAddr: contractAddr,
			Amount:  tlb.MustFromTON("0.05"),
			Body:    bodyCell,
		},
	}

	tx, _, err := s.wallet.SendManyWaitTransaction(ctx, []*wallet.Message{msg})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(tx.Hash), nil
}