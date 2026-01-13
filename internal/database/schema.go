package database

import (
	"context"
	_ "embed"
	"fmt"
	"log"
)

//go:embed schema.sql
var schemaSQL string

func (db *DB) InitSchema(ctx context.Context) error {
	log.Println("🛠️  Reading schema from file...")

	log.Println("🛠️  Applying database schema...")
	_, err := db.pool.Exec(ctx, schemaSQL)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	
	log.Println("✅ Database schema applied")
	return nil
}