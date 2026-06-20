package storage

import (
	"fmt"
	"path/filepath"

	"evmwalletbot/config"
)

type dataPathProvider interface {
	DataPath() string
}

// StorageLabel returns a human-readable storage backend label for UI previews.
func StorageLabel(store Storage, cfg *config.Config) string {
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
