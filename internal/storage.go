package internal

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"
)

// ============================================================================
// Storage Interface Definition
// ============================================================================

// Storage defines the interface for wallet persistence operations.
// All storage backends (SQLite, PostgreSQL) must implement this interface.
type Storage interface {
	// SaveWallets persists a batch of wallets and returns their assigned IDs.
	// Returns the list of IDs in the same order as the input wallets.
	SaveWallets(ctx context.Context, wallets []*Wallet) ([]int64, error)

	// GetWalletByID retrieves a wallet by its database ID.
	GetWalletByID(ctx context.Context, id int64) (*WalletRecord, error)

	// GetWalletByAddress retrieves a wallet by its Ethereum address.
	GetWalletByAddress(ctx context.Context, address []byte) (*WalletRecord, error)

	// CountWallets returns the total number of wallets in storage.
	CountWallets(ctx context.Context) (int64, error)

	// GetStats returns aggregate statistics about stored wallets.
	GetStats(ctx context.Context) (*Stats, error)

	// HealthCheck verifies the storage backend is accessible and operational.
	HealthCheck(ctx context.Context) error

	// GetPoolStats returns connection pool statistics.
	// Returns nil for backends without connection pooling (e.g., SQLite).
	GetPoolStats() *PoolStats

	// Migrate runs schema migrations to ensure the storage schema is up to date.
	Migrate(ctx context.Context) error

	// Close releases all resources held by the storage backend.
	Close() error

	// StorageType returns the backend type identifier (e.g., "sqlite", "postgres").
	StorageType() string
}

// WalletRecord represents a wallet record retrieved from storage.
type WalletRecord struct {
	ID              int64                  // Database-assigned unique identifier
	Address         []byte                 // 20-byte Ethereum address
	PrivateKey      []byte                 // 32-byte secp256k1 private key
	CreatedAt       time.Time              // Timestamp when wallet was created
	Status          int                    // 0=unused, 1=used, 2=reserved
	Metadata        map[string]interface{} // Optional JSON metadata
	DerivationIndex *uint32                // Optional: BIP-44 derivation index (nil for random wallets)
	DerivationPath  string                 // Optional: BIP-44 derivation path (empty for random wallets)
}

// Stats contains aggregate statistics about stored wallets.
type Stats struct {
	TotalWallets    int64     // Total number of wallets
	UnusedWallets   int64     // Wallets with status=0
	UsedWallets     int64     // Wallets with status=1
	ReservedWallets int64     // Wallets with status=2
	OldestWallet    time.Time // Timestamp of oldest wallet
	NewestWallet    time.Time // Timestamp of newest wallet
	WalletsToday    int64     // Wallets created since midnight (local DB date)
	TotalEvents     int64     // Event log entries (PostgreSQL only; 0 for SQLite)
	DBSizeBytes     int64     // Total storage size in bytes
}

// PoolStats contains connection pool statistics.
// Only applicable to backends with connection pooling (e.g., PostgreSQL).
type PoolStats struct {
	TotalConns    int32 // Total number of connections in the pool
	IdleConns     int32 // Number of idle connections
	AcquiredConns int32 // Number of connections currently in use
	MaxConns      int32 // Maximum number of connections allowed
}

// Usage returns the pool usage as a percentage (0.0 to 1.0).
func (p *PoolStats) Usage() float64 {
	if p.MaxConns == 0 {
		return 0.0
	}
	return float64(p.AcquiredConns) / float64(p.MaxConns)
}

// ============================================================================
// Storage Factory
// ============================================================================

// NewStorage creates a storage backend based on configuration.
// Returns embedded SQLite by default, PostgreSQL only if explicitly enabled.
// If PostgreSQL is enabled but unavailable, falls back to SQLite with a warning.
func NewStorage(ctx context.Context, cfg *Config) (Storage, error) {
	switch cfg.StorageType {
	case "postgres":
		log.Println("[INFO] PostgreSQL storage requested (opt-in mode)")
		store, err := NewPostgresStorage(ctx, cfg)
		if err != nil {
			log.Printf("[WARN] PostgreSQL unavailable: %v", err)
			log.Println("[INFO] Falling back to embedded SQLite storage")
			return newSQLiteFallback(cfg)
		}
		log.Println("[INFO] Using PostgreSQL storage")
		return store, nil

	case "sqlite", "":
		log.Println("[INFO] Using embedded SQLite storage (zero-setup mode)")
		return NewSQLiteStorage(cfg.DataDir)

	default:
		return nil, fmt.Errorf("unknown storage type: %s (valid options: sqlite, postgres)", cfg.StorageType)
	}
}

// newSQLiteFallback creates a SQLite storage backend as a fallback.
// This is used when PostgreSQL is requested but unavailable.
func newSQLiteFallback(cfg *Config) (Storage, error) {
	store, err := NewSQLiteStorage(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("fallback to SQLite failed: %w", err)
	}
	return store, nil
}

// ============================================================================
// Storage Label Helper
// ============================================================================

type dataPathProvider interface {
	DataPath() string
}

// StorageLabel returns a human-readable storage backend label for UI previews.
func StorageLabel(store Storage, cfg *Config) string {
	switch store.StorageType() {
	case "postgres":
		return fmt.Sprintf("postgres (%s)", cfg.DBName)
	case "sqlite":
		if p, ok := store.(dataPathProvider); ok {
			return fmt.Sprintf("sqlite (%s)", filepath.Base(p.DataPath()))
		}
		if cfg.DataDir != "" {
			return fmt.Sprintf("sqlite (%s)", filepath.Join(cfg.DataDir, "wallets.db"))
		}
		return "sqlite (wallets.db)"
	default:
		return store.StorageType()
	}
}
