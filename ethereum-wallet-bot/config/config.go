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
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string
	Workers    int
	BatchSize  int
	LogLevel   string
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

	return &Config{
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     port,
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "walletdb"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),
		Workers:    workers,
		BatchSize:  batchSize,
		LogLevel:   getEnv("LOG_LEVEL", "info"),
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
