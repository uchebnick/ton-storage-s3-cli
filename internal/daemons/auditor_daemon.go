package daemons

import (
	"context"
	"fmt"
	"log"
	"time"

	"ton-storage-s3-cli/internal/database"	// Замените на ваш путь к пакету database
	"ton-storage-s3-cli/internal/ton"	// Замените на ваш путь к пакету ton
)

func RunAuditorWorker(ctx context.Context, workerID int, totalWorkers int, db *database.DB, tonSvc *ton.Service) {
	log.Printf("[Auditor %d] Worker started. Monitoring contracts...", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Auditor %d] Stopping...", workerID)
			return
		default:
		}

		contracts, err := db.GetActiveContracts(ctx, totalWorkers, workerID)
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

	logPrefix := fmt.Sprintf("[Auditor %d | %s]", workerID, c.ContractAddr)

	report, err := tonSvc.AuditProvider(ctx, c.BagID, c.ProviderAddr)
	if err != nil {
		log.Printf("%s Audit check skipped due to error: %v", logPrefix, err)
		return
	}

	if report.IsHealthy {

		if err := db.UpdateContractCheck(ctx, c.ID); err != nil {
			log.Printf("%s Failed to update DB check time: %v", logPrefix, err)
		}
		return
	}

	log.Printf("%s 🚨 PROVIDER DEAD. Reason: %s. Status: %s. LastProof: %s ago",
		logPrefix, report.FailureReason, report.Status, time.Since(report.LastProofAt))

	txHash, err := tonSvc.WithdrawFunds(ctx, c.BagID, c.ProviderAddr)
	if err != nil {
		log.Printf("%s Withdraw failed (network error?): %v", logPrefix, err)
	} else {
		log.Printf("%s 💸 Funds withdrawn successfully! Tx: %s", logPrefix, txHash)
	}

	if err := db.MarkContractFailed(ctx, c.ID); err != nil {
		log.Printf("%s Critical DB Error: Failed to mark contract as failed: %v", logPrefix, err)
	} else {
		log.Printf("%s Contract marked as FAILED. Replicator will handle replacement.", logPrefix)
	}
}
