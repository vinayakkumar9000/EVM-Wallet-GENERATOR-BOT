// Package sqlite provides an embedded SQLite storage backend.
// This is the default storage backend requiring no external services.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver

	"evmwalletbot/storage"
	"evmwalletbot/wallet"
)

// SQLiteStorage implements the storage.Storage interface using embedded SQLite.
type SQLiteStorage struct {
	db       *sql.DB
	dataPath string
}

// NewSQLiteStorage creates a new SQLite storage backend.
// If dataDir is empty, it auto-determines a suitable location.
func NewSQLiteStorage(dataDir string) (*SQLiteStorage, error) {
	// Determine data directory
	if dataDir == "" {
		var err error
		dataDir, err = determineDataDir()
		if err != nil {
			return nil, fmt.Errorf("determine data directory: %w", err)
		}
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	// Database file path
	dbPath := filepath.Join(dataDir, "wallets.db")

	// Open SQLite database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite works best with single writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	return &SQLiteStorage{
		db:       db,
		dataPath: dbPath,
	}, nil
}

// determineDataDir finds a suitable directory for the database file.
// Priority: next to executable > user config dir
func determineDataDir() (string, error) {
	// Try next to executable first
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		// Check if we can write to this directory
		testFile := filepath.Join(exeDir, ".write_test")
		if f, err := os.Create(testFile); err == nil {
			f.Close()
			os.Remove(testFile)
			return exeDir, nil
		}
	}

	// Fallback to user config directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get user config dir: %w", err)
	}

	appDir := filepath.Join(configDir, "evmwalletbot")
	return appDir, nil
}

// Migrate runs schema migrations to create tables and indexes.
func (s *SQLiteStorage) Migrate(ctx context.Context) error {
	// Create wallets table
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS wallets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			address BLOB NOT NULL,
			private_key BLOB NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status INTEGER NOT NULL DEFAULT 0,
			metadata TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("create wallets table: %w", err)
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_wallets_address ON wallets(address)",
		"CREATE INDEX IF NOT EXISTS idx_wallets_status ON wallets(status)",
		"CREATE INDEX IF NOT EXISTS idx_wallets_created_at ON wallets(created_at)",
	}

	for _, idx := range indexes {
		if _, err := s.db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	return nil
}

// SaveWallets persists a batch of wallets and returns their assigned IDs.
func (s *SQLiteStorage) SaveWallets(ctx context.Context, wallets []*wallet.Wallet) ([]int64, error) {
	if len(wallets) == 0 {
		return nil, nil
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare insert statement
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO wallets (address, private_key, created_at, status)
		VALUES (?, ?, ?, 0)
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	// Insert wallets and collect IDs
	ids := make([]int64, 0, len(wallets))
	now := time.Now()

	for _, w := range wallets {
		result, err := stmt.ExecContext(ctx, w.Address, w.PrivateKey, now)
		if err != nil {
			return nil, fmt.Errorf("insert wallet: %w", err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("get last insert id: %w", err)
		}

		ids = append(ids, id)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return ids, nil
}

// GetWalletByID retrieves a wallet by its database ID.
func (s *SQLiteStorage) GetWalletByID(ctx context.Context, id int64) (*storage.WalletRecord, error) {
	var record storage.WalletRecord
	var metadataJSON sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, address, private_key, created_at, status, metadata
		FROM wallets
		WHERE id = ?
	`, id).Scan(
		&record.ID,
		&record.Address,
		&record.PrivateKey,
		&record.CreatedAt,
		&record.Status,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("wallet not found: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query wallet: %w", err)
	}

	// Parse metadata JSON if present
	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &record.Metadata); err != nil {
			return nil, fmt.Errorf("parse metadata: %w", err)
		}
	}

	return &record, nil
}

// GetWalletByAddress retrieves a wallet by its Ethereum address.
func (s *SQLiteStorage) GetWalletByAddress(ctx context.Context, address []byte) (*storage.WalletRecord, error) {
	var record storage.WalletRecord
	var metadataJSON sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, address, private_key, created_at, status, metadata
		FROM wallets
		WHERE address = ?
	`, address).Scan(
		&record.ID,
		&record.Address,
		&record.PrivateKey,
		&record.CreatedAt,
		&record.Status,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("wallet not found: address=%x", address)
	}
	if err != nil {
		return nil, fmt.Errorf("query wallet: %w", err)
	}

	// Parse metadata JSON if present
	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &record.Metadata); err != nil {
			return nil, fmt.Errorf("parse metadata: %w", err)
		}
	}

	return &record, nil
}

// CountWallets returns the total number of wallets in storage.
func (s *SQLiteStorage) CountWallets(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM wallets").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count wallets: %w", err)
	}
	return count, nil
}

// GetStats returns aggregate statistics about stored wallets.
func (s *SQLiteStorage) GetStats(ctx context.Context) (*storage.Stats, error) {
	stats := &storage.Stats{}

	// Get total count
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM wallets").Scan(&stats.TotalWallets)
	if err != nil {
		return nil, fmt.Errorf("count total wallets: %w", err)
	}

	// If no wallets, return early
	if stats.TotalWallets == 0 {
		return stats, nil
	}

	// Get counts by status
	err = s.db.QueryRowContext(ctx, `
		SELECT 
			COUNT(CASE WHEN status = 0 THEN 1 END) as unused,
			COUNT(CASE WHEN status = 1 THEN 1 END) as used,
			COUNT(CASE WHEN status = 2 THEN 1 END) as reserved
		FROM wallets
	`).Scan(&stats.UnusedWallets, &stats.UsedWallets, &stats.ReservedWallets)
	if err != nil {
		return nil, fmt.Errorf("count by status: %w", err)
	}

	// Get oldest and newest timestamps
	err = s.db.QueryRowContext(ctx, `
		SELECT MIN(created_at), MAX(created_at) FROM wallets
	`).Scan(&stats.OldestWallet, &stats.NewestWallet)
	if err != nil {
		return nil, fmt.Errorf("get timestamps: %w", err)
	}

	return stats, nil
}

// HealthCheck verifies the storage backend is accessible and operational.
func (s *SQLiteStorage) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// GetPoolStats returns nil for SQLite (no connection pooling).
func (s *SQLiteStorage) GetPoolStats() *storage.PoolStats {
	return nil // SQLite doesn't use connection pooling
}

// Close releases all resources held by the storage backend.
func (s *SQLiteStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// StorageType returns the backend type identifier.
func (s *SQLiteStorage) StorageType() string {
	return "sqlite"
}

// DataPath returns the path to the SQLite database file.
func (s *SQLiteStorage) DataPath() string {
	return s.dataPath
}
