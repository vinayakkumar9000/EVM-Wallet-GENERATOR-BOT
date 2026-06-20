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
	// Storage configuration
	StorageType string // ponytail: Storage backend - "sqlite" (default) or "postgres"
	DataDir     string // ponytail: Data directory for SQLite database (auto-determined if empty)

	// PostgreSQL configuration (only used if StorageType=postgres)
	DBHost               string
	DBPort               int
	DBUser               string
	DBPassword           string
	DBName               string
	DBSSLMode            string
	DBMaxConns           int     // ponytail: Configurable connection pool size
	DBMinConns           int     // ponytail: Configurable minimum connections
	PoolMonitorInterval  int     // ponytail: Pool monitoring interval in seconds (0 to disable)
	PoolWarningThreshold float64 // ponytail: Pool usage warning threshold (0.0-1.0, default 0.8)

	// Generation configuration
	Workers       int
	BatchSize     int
	LogLevel      string
	EnableLogging bool // ponytail: Optional logging, reduces I/O overhead

	// UI configuration
	UIMode           string // ponytail: UI display mode - "full" or "minimal" (default: full)
	ShowFirstRunTips bool   // ponytail: Show tips on first run (default: true)

	// Export configuration
	ExportEnabled       bool   // ponytail: Enable plaintext file export (default: false)
	ExportMode          string // ponytail: Export mode - "paired", "key-only", "address-only", "combined" (default: paired)
	ExportDir           string // ponytail: Output directory for export files (default: ./exports)
	ExportOverwrite     bool   // ponytail: Overwrite existing files (default: false, append mode)
	ExportAddressPrefix bool   // ponytail: Add 0x prefix to addresses (default: true)
	ExportKeyPrefix     bool   // ponytail: Add 0x prefix to private keys (default: true)
	ExportUseChecksum   bool   // ponytail: Use EIP-55 checksum for addresses (default: true)
}

// Load reads .env (if present) then falls back to real environment variables.
// Returns a config with safe defaults - never fails if .env is missing.
func Load() (*Config, error) {
	// Best-effort .env load — no error if file is absent.
	_ = godotenv.Load()

	// Storage configuration
	storageType := getEnv("STORAGE", "sqlite")
	if storageType != "sqlite" && storageType != "postgres" {
		storageType = "sqlite" // Default to sqlite for invalid values
	}
	dataDir := getEnv("WALLET_DATA_DIR", "") // Empty = auto-determined

	// PostgreSQL configuration (only validated if storageType=postgres)
	port, err := strconv.Atoi(getEnv("DB_PORT", "5432"))
	if err != nil {
		port = 5432
	}

	// Generation configuration
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

	uiMode := getEnv("UI_MODE", "full")
	if uiMode != "full" && uiMode != "minimal" {
		uiMode = "full"
	}

	showFirstRunTips := getEnv("SHOW_FIRST_RUN_TIPS", "true") == "true"

	// Export configuration
	exportEnabled := getEnv("EXPORT_ENABLED", "false") == "true"
	exportMode := getEnv("EXPORT_MODE", "paired")
	if exportMode != "paired" && exportMode != "key-only" && exportMode != "address-only" && exportMode != "combined" {
		exportMode = "paired"
	}
	exportDir := getEnv("EXPORT_DIR", "./exports")
	exportOverwrite := getEnv("EXPORT_OVERWRITE", "false") == "true"
	exportAddressPrefix := getEnv("EXPORT_ADDRESS_PREFIX", "true") == "true"
	exportKeyPrefix := getEnv("EXPORT_KEY_PREFIX", "true") == "true"
	exportUseChecksum := getEnv("EXPORT_USE_CHECKSUM", "true") == "true"

	cfg := &Config{
		StorageType:          storageType,
		DataDir:              dataDir,
		DBHost:               getEnv("DB_HOST", "localhost"),
		DBPort:               port,
		DBUser:               getEnv("DB_USER", "postgres"),
		DBPassword:           getEnv("DB_PASSWORD", ""),
		DBName:               getEnv("DB_NAME", "walletdb"),
		DBSSLMode:            getEnv("DB_SSLMODE", "disable"),
		DBMaxConns:           maxConns,
		DBMinConns:           minConns,
		PoolMonitorInterval:  poolMonitorInterval,
		PoolWarningThreshold: poolWarningThreshold,
		Workers:              workers,
		BatchSize:            batchSize,
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		EnableLogging:        enableLogging,
		UIMode:               uiMode,
		ShowFirstRunTips:     showFirstRunTips,
		ExportEnabled:        exportEnabled,
		ExportMode:           exportMode,
		ExportDir:            exportDir,
		ExportOverwrite:      exportOverwrite,
		ExportAddressPrefix:  exportAddressPrefix,
		ExportKeyPrefix:      exportKeyPrefix,
		ExportUseChecksum:    exportUseChecksum,
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks configuration for invalid values and constraint violations.
// ponytail: Fail fast on invalid config rather than runtime errors.
func (c *Config) Validate() error {
	// Storage type validation
	if c.StorageType != "sqlite" && c.StorageType != "postgres" {
		return fmt.Errorf("STORAGE must be either 'sqlite' or 'postgres', got '%s'", c.StorageType)
	}

	// PostgreSQL-specific validation (only if using postgres)
	if c.StorageType == "postgres" {
		if c.DBMinConns > c.DBMaxConns {
			return fmt.Errorf("DB_MIN_CONNS (%d) cannot exceed DB_MAX_CONNS (%d)", c.DBMinConns, c.DBMaxConns)
		}
		if c.DBMaxConns < 1 {
			return fmt.Errorf("DB_MAX_CONNS must be at least 1, got %d", c.DBMaxConns)
		}
		if c.DBMinConns < 0 {
			return fmt.Errorf("DB_MIN_CONNS cannot be negative, got %d", c.DBMinConns)
		}
		if c.PoolMonitorInterval < 0 {
			return fmt.Errorf("POOL_MONITOR_INTERVAL cannot be negative, got %d", c.PoolMonitorInterval)
		}
		if c.PoolWarningThreshold <= 0 || c.PoolWarningThreshold > 1.0 {
			return fmt.Errorf("POOL_WARNING_THRESHOLD must be between 0.0 and 1.0, got %f", c.PoolWarningThreshold)
		}
		if c.DBPort < 1 || c.DBPort > 65535 {
			return fmt.Errorf("DB_PORT must be between 1 and 65535, got %d", c.DBPort)
		}
		if c.DBHost == "" {
			return fmt.Errorf("DB_HOST cannot be empty")
		}
		if c.DBUser == "" {
			return fmt.Errorf("DB_USER cannot be empty")
		}
		if c.DBName == "" {
			return fmt.Errorf("DB_NAME cannot be empty")
		}
	}

	// Worker and batch size constraints (applies to all storage types)
	if c.Workers < 1 {
		return fmt.Errorf("WORKERS must be at least 1, got %d", c.Workers)
	}
	if c.BatchSize < 1 {
		return fmt.Errorf("BATCH_SIZE must be at least 1, got %d", c.BatchSize)
	}
	if c.BatchSize > 1000 {
		return fmt.Errorf("BATCH_SIZE cannot exceed 1000, got %d", c.BatchSize)
	}

	// UI mode constraints
	if c.UIMode != "full" && c.UIMode != "minimal" {
		return fmt.Errorf("UI_MODE must be either 'full' or 'minimal', got '%s'", c.UIMode)
	}

	// Export mode constraints
	if c.ExportMode != "paired" && c.ExportMode != "key-only" && c.ExportMode != "address-only" && c.ExportMode != "combined" {
		return fmt.Errorf("EXPORT_MODE must be one of 'paired', 'key-only', 'address-only', or 'combined', got '%s'", c.ExportMode)
	}
	if c.ExportEnabled && c.ExportDir == "" {
		return fmt.Errorf("EXPORT_DIR cannot be empty when EXPORT_ENABLED is true")
	}

	return nil
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
