package database

import (
	"context"
	"fmt"
	"log"
	"os"
)

func (db *DB) InitSchema(ctx context.Context) error {
	log.Println("🛠️  Reading schema from file...")

	content, err := os.ReadFile("./internal/database/schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	log.Println("🛠️  Applying database schema...")
	_, err = db.pool.Exec(ctx, string(content))
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	
	log.Println("✅ Database schema applied")
	return nil
}