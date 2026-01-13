package database

import (
	"context"
	"fmt"
	"log"
)

const SchemaSQL = `
-- 1. Таблица API Ключей
CREATE TABLE IF NOT EXISTS api_keys (
    access_key VARCHAR(50) PRIMARY KEY,
    secret_key VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- 2. Таблица Бакетов
CREATE TABLE IF NOT EXISTS buckets (
    name VARCHAR(63) PRIMARY KEY,
    created_at TIMESTAMP DEFAULT NOW()
);

-- 3. Таблица Файлов
CREATE TABLE IF NOT EXISTS files (
    id BIGSERIAL PRIMARY KEY,
    bucket_name VARCHAR(255) NOT NULL,
    object_key VARCHAR(1024) NOT NULL,
    bag_id VARCHAR(64) NOT NULL,
    size_bytes BIGINT NOT NULL,
    target_replicas INT DEFAULT 3,
    status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT NOW(),
    
    -- Уникальность имени файла внутри бакета
    UNIQUE(bucket_name, object_key),

    -- Внешний ключ: Если удалить бакет, удалятся и файлы (CASCADE)
    CONSTRAINT fk_bucket 
        FOREIGN KEY (bucket_name) 
        REFERENCES buckets(name) 
        ON DELETE CASCADE 
        ON UPDATE CASCADE
);

-- 4. Таблица Контрактов
CREATE TABLE IF NOT EXISTS contracts (
    id BIGSERIAL PRIMARY KEY,
    file_id BIGINT REFERENCES files(id) ON DELETE CASCADE,
    
    provider_addr VARCHAR(255) NOT NULL,
    contract_addr VARCHAR(255) NOT NULL,
    balance_nano_ton BIGINT DEFAULT 0,
    status VARCHAR(50) DEFAULT 'active',
    last_check TIMESTAMP DEFAULT NOW(),
    created_at TIMESTAMP DEFAULT NOW()
);

-- 5. Индексы
CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);
CREATE INDEX IF NOT EXISTS idx_contracts_status_check ON contracts(status, last_check);
`

func (db *DB) InitSchema(ctx context.Context) error {
	log.Println("🛠️  Applying database schema...")
	_, err := db.pool.Exec(ctx, SchemaSQL)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	log.Println("✅ Database schema applied")
	return nil
}
