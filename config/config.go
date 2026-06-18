// Package config loads application configuration from .env / environment.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration values.
type Config struct {
	DBHost        string
	DBPort        int
	DBUser        string
	DBPassword    string
	DBName        string
	DBSSLMode     string
	DBMaxConns    int  // ponytail: Configurable connection pool size
	DBMinConns    int  // ponytail: Configurable minimum connections
	Workers       int
	BatchSize     int
	LogLevel              string
	EnableLogging         bool    // ponytail: Optional logging, reduces I/O overhead
	PoolMonitorInterval   int     // ponytail: Pool monitoring interval in seconds (0 to disable)
	PoolWarningThreshold  float64 // ponytail: Pool usage warning threshold (0.0-1.0, default 0.8)
}

// Load reads .env (if present) then falls back to real environment variables.
func Load() (*Config, error) {
	// Best-effort .env load — no error if file is absent.
	_ = godotenv.Load()

	port, err := strconv.Atoi(getEnv("DB_PORT", "5432"))
	if err != nil {
		return nil, fmt.Errorf("invalid DB_PORT: %w", err)
	}

	workers, err := strconv.Atoi(getEnv("WORKERS", "16"))
	if err != nil || workers < 1 {
		workers = 16
	}

	batchSize, err := strconv.Atoi(getEnv("BATCH_SIZE", "500"))
	if err != nil || batchSize < 1 {
		batchSize = 500
	}
	if batchSize > 1000 {
		batchSize = 1000 // PostgreSQL parameter limit safety
	}

	enableLogging := getEnv("ENABLE_LOGGING", "true") == "true"

	maxConns, err := strconv.Atoi(getEnv("DB_MAX_CONNS", "30"))
	if err != nil || maxConns < 1 {
		maxConns = 30
	}

	minConns, err := strconv.Atoi(getEnv("DB_MIN_CONNS", "5"))
	if err != nil || minConns < 1 {
		minConns = 5
	}

	poolMonitorInterval, err := strconv.Atoi(getEnv("POOL_MONITOR_INTERVAL", "30"))
	if err != nil || poolMonitorInterval < 0 {
		poolMonitorInterval = 30
	}

	poolWarningThreshold := 0.8
	if thresholdStr := getEnv("POOL_WARNING_THRESHOLD", "0.8"); thresholdStr != "" {
		if parsed, err := strconv.ParseFloat(thresholdStr, 64); err == nil && parsed > 0 && parsed <= 1.0 {
			poolWarningThreshold = parsed
		}
	}

	return &Config{
		DBHost:               getEnv("DB_HOST", "localhost"),
		DBPort:               port,
		DBUser:               getEnv("DB_USER", "postgres"),
		DBPassword:           getEnv("DB_PASSWORD", ""),
		DBName:               getEnv("DB_NAME", "walletdb"),
		DBSSLMode:            getEnv("DB_SSLMODE", "disable"),
		DBMaxConns:           maxConns,
		DBMinConns:           minConns,
		Workers:              workers,
		BatchSize:            batchSize,
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		EnableLogging:        enableLogging,
		PoolMonitorInterval:  poolMonitorInterval,
		PoolWarningThreshold: poolWarningThreshold,
	}, nil
}

// DSN returns a PostgreSQL connection string (lib/pq format).
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
