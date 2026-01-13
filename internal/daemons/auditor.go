package daemons

import (
	"context"
	"fmt"
	"log"
	"time"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"
)

func RunAuditorWorker(ctx context.Context, workerID int, totalWorkers int, db *database.DB, tonSvc *ton.Service) {
	log.Printf("[Auditor %d] Worker started. Monitoring pending & active contracts...", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Auditor %d] Stopping...", workerID)
			return
		default:
		}

		contracts, err := db.GetContractsForAudit(ctx, totalWorkers, workerID)
		if err != nil {
			log.Printf("[Auditor %d] DB Error: %v", workerID, err)
			time.Sleep(5 * time.Second)
			continue
		}

		if len(contracts) == 0 {
			time.Sleep(10 * time.Second)
			continue
		}

		for _, c := range contracts {
			if ctx.Err() != nil {
				return
			}
			processContract(ctx, workerID, db, tonSvc, c)
		}
	}
}

func processContract(ctx context.Context, workerID int, db *database.DB, tonSvc *ton.Service, c database.ContractWithMeta) {
	logPrefix := fmt.Sprintf("[Auditor %d | %s]", workerID, c.ProviderAddr)

	report, err := tonSvc.AuditProvider(ctx, c.BagID, c.ProviderAddr)
	if err != nil {
		log.Printf("%s Check skipped: %v", logPrefix, err)
		return
	}

	if report.IsHealthy {
		if c.Status == "pending" {
			if err := db.MarkContractActive(ctx, c.ID); err != nil {
				log.Printf("%s ❌ Failed to activate contract: %v", logPrefix, err)
			} else {
				log.Printf("%s 🚀 Contract activated! Provider is verified online.", logPrefix)
				
				if err := db.UpgradeFileStatusIfNeeded(ctx, c.FileID); err != nil {
					log.Printf("%s Failed to update file status: %v", logPrefix, err)
				}
			}
		} else {
			if err := db.UpdateContractCheck(ctx, c.ID); err != nil {
				log.Printf("%s Failed to update last_check: %v", logPrefix, err)
			}
		}
		return
	}

	log.Printf("%s 🚨 PROVIDER DEAD (Status: %s). Reason: %s. Removing...", 
		logPrefix, c.Status, report.FailureReason)

	txHash, err := tonSvc.RemoveProvider(ctx, c.BagID, c.ProviderAddr)
	if err != nil {
		log.Printf("%s ⚠️ Failed to remove provider on-chain: %v", logPrefix, err)
	} else {
		log.Printf("%s ✂️ Provider removed on-chain. Tx: %s", logPrefix, txHash)
	}

	if err := db.MarkContractFailed(ctx, c.ID); err != nil {
		log.Printf("%s Critical DB Error marking failed: %v", logPrefix, err)
	} else {
		if err := db.DowngradeFileStatusIfNeeded(ctx, c.FileID); err != nil {
			log.Printf("%s Failed to downgrade file status: %v", logPrefix, err)
		}
	}
}