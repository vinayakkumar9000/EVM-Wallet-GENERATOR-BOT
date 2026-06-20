package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"evmwalletbot/storage"
	"evmwalletbot/storage/sqlite"
	"evmwalletbot/wallet"
)

func TestSQLiteStorageSuite(t *testing.T) {
	runStorageSuite(t, func(t *testing.T) (storage.Storage, func()) {
		dir := t.TempDir()
		store, err := sqlite.NewSQLiteStorage(dir)
		if err != nil {
			t.Fatalf("NewSQLiteStorage: %v", err)
		}
		cleanup := func() { _ = store.Close() }
		return store, cleanup
	})
}

func TestPostgresStorageSuite(t *testing.T) {
	if os.Getenv("POSTGRES_TEST") != "1" {
		t.Skip("set POSTGRES_TEST=1 to run PostgreSQL storage tests")
	}
	t.Skip("PostgreSQL integration test requires ephemeral Postgres setup")
}

func TestSQLiteCreatesDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.NewSQLiteStorage(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "wallets.db")); err != nil {
		t.Fatalf("expected wallets.db in data dir: %v", err)
	}
}

func runStorageSuite(t *testing.T, setup func(t *testing.T) (storage.Storage, func())) {
	t.Helper()

	store, cleanup := setup(t)
	defer cleanup()

	ctx := context.Background()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if err := store.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}

	if store.StorageType() != "sqlite" {
		t.Fatalf("StorageType = %q, want sqlite", store.StorageType())
	}

	count, err := store.CountWallets(ctx)
	if err != nil {
		t.Fatalf("CountWallets: %v", err)
	}
	if count != 0 {
		t.Fatalf("initial CountWallets = %d, want 0", count)
	}

	w, err := wallet.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ids, err := store.SaveWallets(ctx, []*wallet.Wallet{w})
	if err != nil {
		t.Fatalf("SaveWallets: %v", err)
	}
	if len(ids) != 1 || ids[0] < 1 {
		t.Fatalf("SaveWallets ids = %v, want one positive id", ids)
	}

	record, err := store.GetWalletByID(ctx, ids[0])
	if err != nil {
		t.Fatalf("GetWalletByID: %v", err)
	}
	if len(record.Address) != 20 {
		t.Fatalf("address length = %d, want 20", len(record.Address))
	}

	recordByAddr, err := store.GetWalletByAddress(ctx, w.Address)
	if err != nil {
		t.Fatalf("GetWalletByAddress: %v", err)
	}
	if recordByAddr.ID != ids[0] {
		t.Fatalf("GetWalletByAddress id = %d, want %d", recordByAddr.ID, ids[0])
	}

	count, err = store.CountWallets(ctx)
	if err != nil {
		t.Fatalf("CountWallets after insert: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountWallets after insert = %d, want 1", count)
	}

	stats, err := store.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalWallets != 1 || stats.UnusedWallets != 1 {
		t.Fatalf("GetStats = %+v, want 1 total/unused", stats)
	}

	if store.GetPoolStats() != nil {
		t.Fatal("SQLite GetPoolStats should return nil")
	}
}
