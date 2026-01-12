package daemons

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"
)


func RunPingerWorker(ctx context.Context, workerID int, totalWorkers int, db *database.DB, tonSvc *ton.Service) {
	log.Printf("[Pinger %d] Worker started. Pinging active providers... 🔔", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Pinger %d] Stopping...", workerID)
			return
		default:
		}

		contracts, err := db.GetActiveContracts(ctx, totalWorkers, workerID)
		if err != nil {
			log.Printf("[Pinger %d] DB Error: %v", workerID, err)
			continue
		}

		if len(contracts) == 0 {
			continue
		}

		for _, c := range contracts {
			if ctx.Err() != nil {
				return
			}
			processPing(ctx, workerID, tonSvc, c)
		}

		time.Sleep(5 * time.Second)
	}
}

func processPing(ctx context.Context, workerID int, tonSvc *ton.Service, c database.ContractWithMeta) {
	logPrefix := fmt.Sprintf("[Pinger %d | %s]", workerID, c.ProviderAddr)

	bagID, err := hex.DecodeString(c.BagID)
	if err != nil {
		log.Printf("%s ❌ Invalid Bag ID hex: %v", logPrefix, err)
		return
	}

	log.Printf("%s 🔔 Pinging provider...", logPrefix)
	
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err = tonSvc.PingProvider(pingCtx, bagID, c.ProviderAddr)
	if err != nil {
		log.Printf("%s ⚠️ Ping failed: %v", logPrefix, err)
	} else {
		log.Printf("%s ✅ Pong! Provider is alive and aware.", logPrefix)
	}
}