-- Ключи для S3 авторизации
CREATE TABLE api_keys (
                          access_key VARCHAR(50) PRIMARY KEY,
                          secret_key VARCHAR(100) NOT NULL,
                          created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE buckets (
                         name VARCHAR(63) PRIMARY KEY, -- S3 бакеты уникальны глобально
                         created_at TIMESTAMP DEFAULT NOW()
);

-- Файлы
CREATE TABLE files (
                       id BIGSERIAL PRIMARY KEY,
                       bucket_name VARCHAR(255) NOT NULL,
                       object_key VARCHAR(1024) NOT NULL,
                       bag_id VARCHAR(64) NOT NULL,
                       size_bytes BIGINT NOT NULL,
                       target_replicas INT DEFAULT 3,
                       status VARCHAR(50) DEFAULT 'pending',
                       created_at TIMESTAMP DEFAULT NOW(),
                       UNIQUE(bucket_name, object_key)
);

-- Пример правильного Foreign Key (если вы еще можете поправить миграцию)
ALTER TABLE files
DROP CONSTRAINT IF EXISTS files_bucket_name_fkey,
ADD CONSTRAINT files_bucket_name_fkey
    FOREIGN KEY (bucket_name)
    REFERENCES buckets(name)
    ON DELETE CASCADE;

-- Контракты
CREATE TABLE contracts (
                           id BIGSERIAL PRIMARY KEY,       -- Тоже int64
                           file_id BIGINT REFERENCES files(id) ON DELETE CASCADE,
                           provider_addr VARCHAR(255) NOT NULL,
                           contract_addr VARCHAR(255) NOT NULL,
                           balance_nano_ton BIGINT DEFAULT 0,
                           status VARCHAR(50) DEFAULT 'active',
                           last_check TIMESTAMP DEFAULT NOW(),
                           created_at TIMESTAMP DEFAULT NOW()
);

-- Индексы для ускорения выборок
CREATE INDEX idx_files_status ON files(status);
CREATE INDEX idx_contracts_status_check ON contracts(status, last_check);