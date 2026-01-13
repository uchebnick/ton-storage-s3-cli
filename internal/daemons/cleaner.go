package daemons

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"
)

func RunCleanerWorker(ctx context.Context, workerID int, totalWorkers int, db *database.DB, tonSvc *ton.Service) {
	log.Printf("[Cleaner %d] Worker started. Monitoring redundancy for offloading... 🧹", workerID)

	minAge := 2 * time.Minute
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-minAge)

			files, err := db.GetFilesReadyForCleaning(ctx, cutoff, totalWorkers, workerID, 50)
			if err != nil {
				log.Printf("[Cleaner %d] DB Error: %v", workerID, err)
				continue
			}

			for _, f := range files {
				bagBytes, err := hex.DecodeString(f.BagID)
				if err != nil {
					continue
				}

				err = tonSvc.DeleteLocalFile(bagBytes)
				if err != nil {
					log.Printf("[Cleaner %d] ❌ Failed to offload %s: %v", workerID, f.ObjectKey, err)
				} else {
					// log.Printf("[Cleaner %d] 🧹 Offloaded: %s", workerID, f.ObjectKey)
				}
			}
		}
	}
}