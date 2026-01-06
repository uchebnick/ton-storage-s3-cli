package daemons

import (
	"context"
	"encoding/hex"	// <--- Добавлен импорт
	"log"
	"math/big"
	"math/rand"
	"time"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"

	"github.com/xssnick/tonutils-go/tlb"
)

func RunReplicatorWorker(ctx context.Context, workerID int, totalWorkers int, db *database.DB, tonSvc *ton.Service) {
	log.Printf("[Replicator %d] Worker started. Monitoring file health 🚑", workerID)

	source := rand.NewSource(time.Now().UnixNano() + int64(workerID))
	rng := rand.New(source)

	for {

		select {
		case <-ctx.Done():
			log.Printf("[Replicator %d] Stopping...", workerID)
			return
		default:
		}

		files, err := db.GetFilesNeedingReplication(ctx, totalWorkers, workerID)
		if err != nil {
			log.Printf("[Replicator %d] DB Error: %v", workerID, err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(files) == 0 {
			time.Sleep(10 * time.Second)
			continue
		}

		for _, f := range files {
			if ctx.Err() != nil {
				return
			}
			processFile(ctx, workerID, db, tonSvc, f, rng)
		}
	}
}

func processFile(ctx context.Context, workerID int, db *database.DB, tonSvc *ton.Service, f database.FileWithStatus, rng *rand.Rand) {
	needed := f.TargetReplicas - f.ActiveReplicas
	if needed <= 0 {
		return
	}

	bagBytes, err := hex.DecodeString(f.BagID)
	if err != nil {
		log.Printf("[Replicator %d] ❌ Critical: Invalid BagID hex in DB (%s): %v", workerID, f.BagID, err)
		return
	}

	log.Printf("[Replicator %d] File %s (ID: %d) needs %d new replicas (Active: %d)",
		workerID, f.BagID, f.ID, needed, f.ActiveReplicas)

	currentExcludes := make([]string, len(f.UsedProviders))
	copy(currentExcludes, f.UsedProviders)

	for i := 0; i < needed; i++ {

		providerAddr, err := tonSvc.FindRandomProvider(ctx, currentExcludes)
		if err != nil {
			log.Printf("[Replicator %d] ⚠️ Failed to find suitable provider: %v", workerID, err)
			break
		}

		balance := calcJitterBalance(rng)

		log.Printf("[Replicator %d] Hiring provider %s for %s...",
			workerID, providerAddr, balance.String())

		contractAddr, err := tonSvc.HireProvider(ctx, bagBytes, providerAddr, balance)
		if err != nil {
			log.Printf("[Replicator %d] ❌ Hire failed: %v", workerID, err)
			continue
		}

		newContract := &database.Contract{
			FileID:		f.ID,
			ProviderAddr:	providerAddr,
			ContractAddr:	contractAddr,
			BalanceNano:	balance.Nano().Int64(),
			Status:		"active",
		}

		if err := db.RegisterContract(ctx, newContract); err != nil {
			log.Printf("[Replicator %d] Critical: Failed to save contract to DB: %v", workerID, err)
		} else {
			log.Printf("[Replicator %d] ✅ Contract created: %s", workerID, contractAddr)
			currentExcludes = append(currentExcludes, providerAddr)
		}
	}
}

func calcJitterBalance(rng *rand.Rand) tlb.Coins {
	const baseNano = 200_000_000
	const maxJitter = 100_000_000

	jitter := rng.Int63n(maxJitter)
	totalNano := big.NewInt(baseNano + jitter)
	return tlb.FromNanoTON(totalNano)
}
