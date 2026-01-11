package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ton-storage-s3-cli/internal/api"
	"ton-storage-s3-cli/internal/config"
	"ton-storage-s3-cli/internal/daemons"
	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"
)

func main() {

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("❌ Config error: %v", err)
	}

	if err := os.MkdirAll(cfg.InternalDBPath, 0755); err != nil {
		log.Fatalf("❌ Failed to create internal db dir: %v", err)
	}
	if err := os.MkdirAll(cfg.DownloadsPath, 0755); err != nil {
		log.Fatalf("❌ Failed to create downloads dir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := database.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("❌ DB Init failed: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(ctx); err != nil {
		log.Fatalf("❌ DB Schema Init failed: %v", err)
	}

	log.Println("✅ Database connected")

	tonSvc, err := ton.NewService(
		ctx,
		cfg.WalletSeed,
		cfg.InternalDBPath,
		cfg.DownloadsPath,
		cfg.ExternalIP,
	)
	if err != nil {
		log.Fatalf("❌ TON Service init failed: %v", err)
	}
	log.Println("✅ TON Service initialized")

	if err := tonSvc.StartSeeding(ctx); err != nil {
		log.Printf("⚠️ Warning: Failed to resume seeding: %v", err)
	}

	replicatorTask := func(ctx context.Context, id int, total int) {
		daemons.RunReplicatorWorker(ctx, id, total, db, tonSvc)
	}

	replicatorPool := daemons.NewPool(ctx, cfg.ReplicatorWorkers, replicatorTask)
	replicatorPool.Start()
	log.Printf("✅ Started Replicator Pool (%d workers)", cfg.ReplicatorWorkers)

	auditorTask := func(ctx context.Context, id int, total int) {
		daemons.RunAuditorWorker(ctx, id, total, db, tonSvc)
	}

	auditorPool := daemons.NewPool(ctx, cfg.AuditorWorkers, auditorTask)
	auditorPool.Start()
	log.Printf("✅ Started Auditor Pool (%d workers)", cfg.AuditorWorkers)

	s3Server := api.NewS3Server(db, tonSvc, cfg.DownloadsPath)
	adminServer := api.NewAdminServer(db, tonSvc)

	go func() {
		if err := s3Server.Start(cfg.ServerPort); err != nil {
			log.Printf("❌ S3 Server Error: %v", err)
			cancel()
		}
	}()

	go func() {
		if err := adminServer.Start(":3000"); err != nil {
			log.Printf("❌ Admin Server Error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("🛑 Received signal: %s. Shutting down...", sig)
	case <-ctx.Done():
		log.Println("🛑 Context cancelled. Shutting down...")
	}

	log.Println("Waiting for Replicators to finish...")
	replicatorPool.Stop()

	log.Println("Waiting for Auditors to finish...")
	auditorPool.Stop()

	cancel()

	log.Println("👋 Shutdown complete.")
}
