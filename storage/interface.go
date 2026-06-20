// Package storage provides an abstraction layer for wallet persistence.
// Supports multiple backends: embedded SQLite (default) and PostgreSQL (optional).
package storage

import (
	"context"
	"time"

	"evmwalletbot/wallet"
)

// Storage defines the interface for wallet persistence operations.
// All storage backends (SQLite, PostgreSQL) must implement this interface.
type Storage interface {
	// SaveWallets persists a batch of wallets and returns their assigned IDs.
	// Returns the list of IDs in the same order as the input wallets.
	SaveWallets(ctx context.Context, wallets []*wallet.Wallet) ([]int64, error)

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
	ID         int64                  // Database-assigned unique identifier
	Address    []byte                 // 20-byte Ethereum address
	PrivateKey []byte                 // 32-byte secp256k1 private key
	CreatedAt  time.Time              // Timestamp when wallet was created
	Status     int                    // 0=unused, 1=used, 2=reserved
	Metadata   map[string]interface{} // Optional JSON metadata
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
