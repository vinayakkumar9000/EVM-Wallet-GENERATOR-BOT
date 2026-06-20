package sqlite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetermineDataDirNotWorkingDirectory(t *testing.T) {
	dir, err := determineDataDir()
	if err != nil {
		t.Fatalf("determineDataDir: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs data dir: %v", err)
	}
	absWD, err := filepath.Abs(wd)
	if err != nil {
		t.Fatalf("Abs wd: %v", err)
	}

	if absDir == absWD {
		t.Fatalf("data dir should not be working directory (%q)", absWD)
	}
}

func TestNewSQLiteStorageUsesConfiguredDataDir(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStorage(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStorage: %v", err)
	}
	defer store.Close()

	want := filepath.Join(dir, "wallets.db")
	if store.DataPath() != want {
		t.Fatalf("DataPath = %q, want %q", store.DataPath(), want)
	}
}
