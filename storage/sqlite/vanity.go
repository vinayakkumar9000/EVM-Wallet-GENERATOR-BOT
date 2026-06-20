// Package sqlite provides an embedded SQLite storage backend.
package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// NewVanitySQLiteStorage creates a new SQLite storage backend for vanity wallets.
// Uses vanity.db instead of wallets.db, same schema.
func NewVanitySQLiteStorage(dataDir string) (*SQLiteStorage, error) {
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

	// Database file path - vanity.db instead of wallets.db
	dbPath := filepath.Join(dataDir, "vanity.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	return &SQLiteStorage{
		db:       db,
		dataPath: dbPath,
	}, nil
}
