package ton

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-storage-provider/pkg/contract"
)

type ProviderReport struct {
	IsHealthy	bool
	Status		string	// "active", "error", "failed", "outdated"
	FailureReason	string
	LastProofAt	time.Time
	Balance		*big.Int
}

func (s *Service) AuditProvider(ctx context.Context, bagIdStr, providerAddrStr string) (*ProviderReport, error) {

	bag, err := hex.DecodeString(bagIdStr)
	if err != nil {
		return nil, fmt.Errorf("invalid bag id: %w", err)
	}
	if len(bag) != 32 {
		return nil, fmt.Errorf("bag id must be 32 bytes")
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
		return nil, fmt.Errorf("bag not found locally")
	}

	contractData, err := s.providerClient.FetchProviderContract(ctx, bag, s.wallet.Address())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch contract data: %w", err)
	}

	var targetProvider *contract.ProviderDataV1
	for _, p := range contractData.Providers {

		if bytes.Equal(p.Key, provAddr.Data()) {
			targetProvider = &p
			break
		}
	}

	if targetProvider == nil {
		return &ProviderReport{
			IsHealthy:	false,
			Status:		"missing",
			FailureReason:	"Provider not found in contract list",
			Balance:	contractData.Balance.Nano(),
		}, nil
	}

	maxSpan := time.Duration(targetProvider.MaxSpan) * time.Second
	lastProofAgo := time.Since(targetProvider.LastProofAt)

	allowedDelay := maxSpan + (maxSpan / 10)

	if lastProofAgo > allowedDelay {
		return &ProviderReport{
			IsHealthy:	false,
			Status:		"outdated",
			FailureReason:	fmt.Sprintf("Proof outdated. Last: %s ago, MaxSpan: %s", lastProofAgo, maxSpan),
			LastProofAt:	targetProvider.LastProofAt,
			Balance:	contractData.Balance.Nano(),
		}, nil
	}

	info, err := s.providerClient.RequestProviderStorageInfo(ctx, bag, targetProvider.Key, s.wallet.Address())

	status := "unknown"
	reason := ""

	if err != nil {
		status = "unreachable"
		reason = err.Error()
	} else {
		status = info.Status
		reason = info.Reason
	}

	isHealthy := (status == "active")
	if !isHealthy && reason == "" {
		reason = fmt.Sprintf("Status is %s", status)
	}

	return &ProviderReport{
		IsHealthy:	isHealthy,
		Status:		status,
		FailureReason:	reason,
		LastProofAt:	targetProvider.LastProofAt,
		Balance:	contractData.Balance.Nano(),
	}, nil
}

func (s *Service) WithdrawFunds(ctx context.Context, bagIdStr, providerAddrStr string) (string, error) {

	bag, err := hex.DecodeString(bagIdStr)
	if err != nil {
		return "", err
	}

	ownerAddr := s.wallet.Address()

	contractAddr, bodyBytes, err := s.providerClient.BuildWithdrawalTransaction(bag, ownerAddr)

	if err != nil {
		return "", fmt.Errorf("build withdraw tx failed: %w", err)
	}

	bodyCell, err := cell.FromBOC(bodyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse body BOC: %w", err)
	}

	msg := &wallet.Message{
		Mode:	1,
		InternalMessage: &tlb.InternalMessage{
			Bounce:		true,
			DstAddr:	contractAddr,
			Amount:		tlb.MustFromTON("0.05"),
			Body:		bodyCell,
		},
	}

	tx, _, err := s.wallet.SendManyWaitTransaction(ctx, []*wallet.Message{msg})
	if err != nil {
		return "", fmt.Errorf("send transaction failed: %w", err)
	}

	return hex.EncodeToString(tx.Hash), nil
}
