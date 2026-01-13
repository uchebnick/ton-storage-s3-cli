package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// База данных
	DatabaseURL	string

	// Настройки TON
	TonConfigURL	string	// Ссылка на global.config.json
	WalletSeed	string	// Сид фраза кошелька (24 слова)
	InternalDBPath	string	// Путь к leveldb самого tonutils-storage
	DownloadsPath	string	// Путь, куда скачиваются файлы

	// Настройки S3 Шлюза
	ServerPort	string
	DefaultReplicas	int

	// Настройки Демонов
	ReplicatorWorkers	int
	AuditorWorkers		int
	PingerWorkers		int
	CleanerWorkers		int
	ExternalIP		string
}

func LoadConfig() (*Config, error) {

	_ = godotenv.Load()

	cfg := &Config{
		DatabaseURL:	getEnv("DB_URL", "postgres://user:pass@localhost:5432/ton_storage?sslmode=disable"),
		TonConfigURL:	getEnv("TON_CONFIG_URL", "https://ton.org/global.config.json"),
		WalletSeed:	getEnv("WALLET_SEED", ""),
		InternalDBPath:	getEnv("INTERNAL_DB_PATH", "./var/ton-storage-db"),
		DownloadsPath:	getEnv("DOWNLOADS_PATH", "./var/downloads"),
		ServerPort:		getEnv("SERVER_PORT", ":8080"),

		DefaultReplicas:	getEnvAsInt("DEFAULT_REPLICAS", 3),
		ReplicatorWorkers:	getEnvAsInt("REPLICATOR_WORKERS", 5),
		AuditorWorkers:		getEnvAsInt("AUDITOR_WORKERS", 3),
		PingerWorkers:		getEnvAsInt("PINGER_WORKERS", 2),
		CleanerWorkers:		getEnvAsInt("CLEANER_WORKERS", 2),
		ExternalIP:		getEnv("EXTERNAL_IP", "0.0.0.0"),
	}

	if cfg.WalletSeed == "" {
		return nil, fmt.Errorf("WALLET_SEED is required")
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultVal
}
