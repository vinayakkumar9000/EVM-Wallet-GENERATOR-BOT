package main

import (
	"context"
	"fmt"
	"log"

	"evmwalletbot/config"
	"evmwalletbot/storage"
	"evmwalletbot/storage/postgres"
	"evmwalletbot/storage/sqlite"
)

// newStorage creates a storage backend based on configuration.
// Returns embedded SQLite by default, PostgreSQL only if explicitly enabled.
// If PostgreSQL is enabled but unavailable, falls back to SQLite with a warning.
func newStorage(ctx context.Context, cfg *config.Config) (storage.Storage, error) {
	switch cfg.StorageType {
	case "postgres":
		log.Println("[INFO] PostgreSQL storage requested (opt-in mode)")
		store, err := postgres.NewPostgresStorage(ctx, cfg)
		if err != nil {
			log.Printf("[WARN] PostgreSQL unavailable: %v", err)
			log.Println("[INFO] Falling back to embedded SQLite storage")
			return newSQLiteFallback(cfg)
		}
		log.Println("[INFO] Using PostgreSQL storage")
		return store, nil

	case "sqlite", "":
		log.Println("[INFO] Using embedded SQLite storage (zero-setup mode)")
		return sqlite.NewSQLiteStorage(cfg.DataDir)

	default:
		return nil, fmt.Errorf("unknown storage type: %s (valid options: sqlite, postgres)", cfg.StorageType)
	}
}

// newSQLiteFallback creates a SQLite storage backend as a fallback.
// This is used when PostgreSQL is requested but unavailable.
func newSQLiteFallback(cfg *config.Config) (storage.Storage, error) {
	store, err := sqlite.NewSQLiteStorage(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("fallback to SQLite failed: %w", err)
	}
	return store, nil
}
