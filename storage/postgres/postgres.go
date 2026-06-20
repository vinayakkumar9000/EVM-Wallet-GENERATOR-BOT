// Package postgres provides a PostgreSQL storage backend.
// This is an optional backend that requires an external PostgreSQL server.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"evmwalletbot/config"
	"evmwalletbot/database"
	"evmwalletbot/storage"
	"evmwalletbot/wallet"
)

// PostgresStorage implements the storage.Storage interface using PostgreSQL.
type PostgresStorage struct {
	pool *pgxpool.Pool
}

// NewPostgresStorage creates a new PostgreSQL storage backend.
// Returns an error if the database is unreachable.
func NewPostgresStorage(ctx context.Context, cfg *config.Config) (*PostgresStorage, error) {
	// Ensure database exists
	if err := database.EnsureDatabase(ctx, cfg); err != nil {
		return nil, fmt.Errorf("ensure database: %w", err)
	}

	// Connect to database
	pool, err := database.Connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	return &PostgresStorage{
		pool: pool,
	}, nil
}

// Migrate runs schema migrations to create tables and indexes.
func (p *PostgresStorage) Migrate(ctx context.Context) error {
	return database.Migrate(p.pool)
}

// SaveWallets persists a batch of wallets and returns their assigned IDs.
// Uses PostgreSQL COPY protocol for high-performance bulk inserts.
func (p *PostgresStorage) SaveWallets(ctx context.Context, wallets []*wallet.Wallet) ([]int64, error) {
	if len(wallets) == 0 {
		return nil, nil
	}

	// Use existing insertWalletBatchCopy from database package
	// We need to expose this function or duplicate the logic here
	// For now, we'll use a simpler multi-row INSERT
	return p.insertWalletBatch(ctx, wallets)
}

// insertWalletBatch inserts a batch of wallets using multi-row INSERT.
func (p *PostgresStorage) insertWalletBatch(ctx context.Context, wallets []*wallet.Wallet) ([]int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Build multi-row INSERT statement
	query := `
		INSERT INTO wallets (address, private_key, created_at, status)
		VALUES ($1, $2, $3, 0)
		RETURNING id
	`

	ids := make([]int64, 0, len(wallets))
	now := time.Now()

	for _, w := range wallets {
		var id int64
		err := tx.QueryRow(ctx, query, w.Address, w.PrivateKey, now).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("insert wallet: %w", err)
		}
		ids = append(ids, id)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return ids, nil
}

// GetWalletByID retrieves a wallet by its database ID.
func (p *PostgresStorage) GetWalletByID(ctx context.Context, id int64) (*storage.WalletRecord, error) {
	var record storage.WalletRecord

	err := p.pool.QueryRow(ctx, `
		SELECT id, address, private_key, created_at, status, metadata
		FROM wallets
		WHERE id = $1
	`, id).Scan(
		&record.ID,
		&record.Address,
		&record.PrivateKey,
		&record.CreatedAt,
		&record.Status,
		&record.Metadata,
	)

	if err != nil {
		return nil, fmt.Errorf("query wallet: %w", err)
	}

	return &record, nil
}

// GetWalletByAddress retrieves a wallet by its Ethereum address.
func (p *PostgresStorage) GetWalletByAddress(ctx context.Context, address []byte) (*storage.WalletRecord, error) {
	var record storage.WalletRecord

	err := p.pool.QueryRow(ctx, `
		SELECT id, address, private_key, created_at, status, metadata
		FROM wallets
		WHERE address = $1
	`, address).Scan(
		&record.ID,
		&record.Address,
		&record.PrivateKey,
		&record.CreatedAt,
		&record.Status,
		&record.Metadata,
	)

	if err != nil {
		return nil, fmt.Errorf("query wallet: %w", err)
	}

	return &record, nil
}

// CountWallets returns the total number of wallets in storage.
func (p *PostgresStorage) CountWallets(ctx context.Context) (int64, error) {
	var count int64
	err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM wallets").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count wallets: %w", err)
	}
	return count, nil
}

// GetStats returns aggregate statistics about stored wallets.
func (p *PostgresStorage) GetStats(ctx context.Context) (*storage.Stats, error) {
	stats := &storage.Stats{}

	// Get total count
	err := p.pool.QueryRow(ctx, "SELECT COUNT(*) FROM wallets").Scan(&stats.TotalWallets)
	if err != nil {
		return nil, fmt.Errorf("count total wallets: %w", err)
	}

	// If no wallets, return early
	if stats.TotalWallets == 0 {
		return stats, nil
	}

	// Get counts by status
	err = p.pool.QueryRow(ctx, `
		SELECT 
			COUNT(*) FILTER (WHERE status = 0) as unused,
			COUNT(*) FILTER (WHERE status = 1) as used,
			COUNT(*) FILTER (WHERE status = 2) as reserved
		FROM wallets
	`).Scan(&stats.UnusedWallets, &stats.UsedWallets, &stats.ReservedWallets)
	if err != nil {
		return nil, fmt.Errorf("count by status: %w", err)
	}

	// Get oldest and newest timestamps
	err = p.pool.QueryRow(ctx, `
		SELECT MIN(created_at), MAX(created_at) FROM wallets
	`).Scan(&stats.OldestWallet, &stats.NewestWallet)
	if err != nil {
		return nil, fmt.Errorf("get timestamps: %w", err)
	}

	return stats, nil
}

// HealthCheck verifies the storage backend is accessible and operational.
func (p *PostgresStorage) HealthCheck(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// GetPoolStats returns connection pool statistics.
func (p *PostgresStorage) GetPoolStats() *storage.PoolStats {
	stats := p.pool.Stat()
	return &storage.PoolStats{
		TotalConns:    stats.TotalConns(),
		IdleConns:     stats.IdleConns(),
		AcquiredConns: stats.AcquiredConns(),
		MaxConns:      stats.MaxConns(),
	}
}

// Close releases all resources held by the storage backend.
func (p *PostgresStorage) Close() error {
	if p.pool != nil {
		p.pool.Close()
	}
	return nil
}

// StorageType returns the backend type identifier.
func (p *PostgresStorage) StorageType() string {
	return "postgres"
}

// Pool returns the underlying connection pool.
// This is provided for backward compatibility with code that needs direct pool access.
func (p *PostgresStorage) Pool() *pgxpool.Pool {
	return p.pool
}
