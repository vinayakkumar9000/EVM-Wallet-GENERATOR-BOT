package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// ============================================================================
// SQLite Storage Implementation
// ============================================================================

// SQLiteStorage implements the Storage interface using embedded SQLite.
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

// determineDataDir finds a suitable directory for the database file.
// Priority: next to executable > user config dir
func determineDataDir() (string, error) {
	// Try next to executable first
	exePath, err := os.Executable()
	if err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(exePath); resolveErr == nil {
			exePath = resolved
		}
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
			address BLOB NOT NULL UNIQUE,
			private_key BLOB NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status INTEGER NOT NULL DEFAULT 0,
			metadata TEXT,
			derivation_index INTEGER,
			derivation_path TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("create wallets table: %w", err)
	}

	// Create indexes (UNIQUE constraint on address already creates an index)
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_wallets_status ON wallets(status)",
		"CREATE INDEX IF NOT EXISTS idx_wallets_created_at ON wallets(created_at)",
	}

	for _, idx := range indexes {
		if _, err := s.db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// Add derivation columns if they don't exist (migration for existing databases)
	// SQLite doesn't support IF NOT EXISTS for ALTER TABLE, so we check first
	var colCount int
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('wallets') 
		WHERE name IN ('derivation_index', 'derivation_path')
	`).Scan(&colCount)
	if err != nil {
		return fmt.Errorf("check derivation columns: %w", err)
	}

	if colCount < 2 {
		// Add missing columns
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE wallets ADD COLUMN derivation_index INTEGER`); err != nil {
			// Ignore error if column already exists
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("add derivation_index column: %w", err)
			}
		}
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE wallets ADD COLUMN derivation_path TEXT`); err != nil {
			// Ignore error if column already exists
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("add derivation_path column: %w", err)
			}
		}
	}

	// Create vanity search state table for resume functionality
	_, err = s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS vanity_search_state (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			patterns TEXT NOT NULL,
			checksum INTEGER NOT NULL DEFAULT 0,
			target_count INTEGER NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			matches_found INTEGER NOT NULL DEFAULT 0,
			start_time DATETIME NOT NULL,
			last_update DATETIME NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'paused', 'completed'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create vanity_search_state table: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_vanity_search_status ON vanity_search_state(status, last_update)
	`)
	if err != nil {
		return fmt.Errorf("create vanity search index: %w", err)
	}

	return nil
}

// SaveWallets persists a batch of wallets and returns their assigned IDs.
func (s *SQLiteStorage) SaveWallets(ctx context.Context, wallets []*Wallet) ([]int64, error) {
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
		INSERT INTO wallets (address, private_key, created_at, status, derivation_index, derivation_path)
		VALUES (?, ?, ?, 0, ?, ?)
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	// Insert wallets and collect IDs
	ids := make([]int64, 0, len(wallets))
	now := time.Now().UTC().Format(time.RFC3339)

	for _, w := range wallets {
		var derivationIndex interface{}
		var derivationPath interface{}

		if w.DerivationIndex != nil {
			derivationIndex = *w.DerivationIndex
		}
		if w.DerivationPath != "" {
			derivationPath = w.DerivationPath
		}

		result, err := stmt.ExecContext(ctx, w.Address, w.PrivateKey, now, derivationIndex, derivationPath)
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
func (s *SQLiteStorage) GetWalletByID(ctx context.Context, id int64) (*WalletRecord, error) {
	var record WalletRecord
	var metadataJSON sql.NullString
	var createdAt sql.NullString
	var derivationIndex sql.NullInt32
	var derivationPath sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, address, private_key, created_at, status, metadata, derivation_index, derivation_path
		FROM wallets
		WHERE id = ?
	`, id).Scan(
		&record.ID,
		&record.Address,
		&record.PrivateKey,
		&createdAt,
		&record.Status,
		&metadataJSON,
		&derivationIndex,
		&derivationPath,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("wallet not found: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query wallet: %w", err)
	}

	record.CreatedAt, err = parseSQLiteTime(createdAt)
	if err != nil {
		return nil, err
	}

	// Parse metadata JSON if present
	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &record.Metadata); err != nil {
			return nil, fmt.Errorf("parse metadata: %w", err)
		}
	}

	// Populate derivation fields if present
	if derivationIndex.Valid {
		idx := uint32(derivationIndex.Int32)
		record.DerivationIndex = &idx
	}
	if derivationPath.Valid {
		record.DerivationPath = derivationPath.String
	}

	return &record, nil
}

// GetWalletByAddress retrieves a wallet by its Ethereum address.
func (s *SQLiteStorage) GetWalletByAddress(ctx context.Context, address []byte) (*WalletRecord, error) {
	var record WalletRecord
	var metadataJSON sql.NullString
	var createdAt sql.NullString
	var derivationIndex sql.NullInt32
	var derivationPath sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, address, private_key, created_at, status, metadata, derivation_index, derivation_path
		FROM wallets
		WHERE address = ?
	`, address).Scan(
		&record.ID,
		&record.Address,
		&record.PrivateKey,
		&createdAt,
		&record.Status,
		&metadataJSON,
		&derivationIndex,
		&derivationPath,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("wallet not found: address=%x", address)
	}
	if err != nil {
		return nil, fmt.Errorf("query wallet: %w", err)
	}

	record.CreatedAt, err = parseSQLiteTime(createdAt)
	if err != nil {
		return nil, err
	}

	// Parse metadata JSON if present
	if metadataJSON.Valid && metadataJSON.String != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &record.Metadata); err != nil {
			return nil, fmt.Errorf("parse metadata: %w", err)
		}
	}

	// Populate derivation fields if present
	if derivationIndex.Valid {
		idx := uint32(derivationIndex.Int32)
		record.DerivationIndex = &idx
	}
	if derivationPath.Valid {
		record.DerivationPath = derivationPath.String
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
func (s *SQLiteStorage) GetStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{}

	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM wallets").Scan(&stats.TotalWallets)
	if err != nil {
		return nil, fmt.Errorf("count total wallets: %w", err)
	}

	if stats.TotalWallets == 0 {
		if info, err := os.Stat(s.dataPath); err == nil {
			stats.DBSizeBytes = info.Size()
		}
		return stats, nil
	}

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

	var oldestRaw, newestRaw sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT MIN(created_at), MAX(created_at) FROM wallets
	`).Scan(&oldestRaw, &newestRaw)
	if err != nil {
		return nil, fmt.Errorf("get timestamps: %w", err)
	}
	stats.OldestWallet, err = parseSQLiteTime(oldestRaw)
	if err != nil {
		return nil, err
	}
	stats.NewestWallet, err = parseSQLiteTime(newestRaw)
	if err != nil {
		return nil, err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM wallets WHERE date(created_at) = date('now', 'localtime')
	`).Scan(&stats.WalletsToday)
	if err != nil {
		return nil, fmt.Errorf("count today's wallets: %w", err)
	}

	if info, err := os.Stat(s.dataPath); err == nil {
		stats.DBSizeBytes = info.Size()
	}

	return stats, nil
}

// HealthCheck verifies the storage backend is accessible and operational.
func (s *SQLiteStorage) HealthCheck(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// GetPoolStats returns nil for SQLite (no connection pooling).
func (s *SQLiteStorage) GetPoolStats() *PoolStats {
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

// parseSQLiteTime parses SQLite datetime strings into time.Time
func parseSQLiteTime(raw sql.NullString) (time.Time, error) {
	if !raw.Valid || raw.String == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw.String); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse sqlite time %q", raw.String)
}

// ============================================================================
// Vanity Search Resume Functionality
// ============================================================================

// VanitySearchState represents a resumable vanity search session
type VanitySearchState struct {
	ID           int64     `json:"id"`
	Patterns     string    `json:"patterns"` // JSON-encoded patterns
	Checksum     bool      `json:"checksum"`
	TargetCount  int       `json:"target_count"`
	Attempts     int64     `json:"attempts"`
	MatchesFound int       `json:"matches_found"`
	StartTime    time.Time `json:"start_time"`
	LastUpdate   time.Time `json:"last_update"`
	Status       string    `json:"status"` // "active", "paused", "completed"
}

// SaveVanitySearchState saves or updates a vanity search state
func (s *SQLiteStorage) SaveVanitySearchState(ctx context.Context, state *VanitySearchState) error {
	query := `
		INSERT INTO vanity_search_state (
			patterns, checksum, target_count, attempts, matches_found,
			start_time, last_update, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			attempts = excluded.attempts,
			matches_found = excluded.matches_found,
			last_update = excluded.last_update,
			status = excluded.status
	`

	result, err := s.db.ExecContext(ctx, query,
		state.Patterns,
		state.Checksum,
		state.TargetCount,
		state.Attempts,
		state.MatchesFound,
		state.StartTime,
		state.LastUpdate,
		state.Status,
	)
	if err != nil {
		return fmt.Errorf("save vanity search state: %w", err)
	}

	if state.ID == 0 {
		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("get last insert id: %w", err)
		}
		state.ID = id
	}

	return nil
}

// GetActiveVanitySearchState retrieves the most recent active/paused search state
func (s *SQLiteStorage) GetActiveVanitySearchState(ctx context.Context) (*VanitySearchState, error) {
	query := `
		SELECT id, patterns, checksum, target_count, attempts, matches_found,
		       start_time, last_update, status
		FROM vanity_search_state
		WHERE status IN ('active', 'paused')
		ORDER BY last_update DESC
		LIMIT 1
	`

	var state VanitySearchState
	err := s.db.QueryRowContext(ctx, query).Scan(
		&state.ID,
		&state.Patterns,
		&state.Checksum,
		&state.TargetCount,
		&state.Attempts,
		&state.MatchesFound,
		&state.StartTime,
		&state.LastUpdate,
		&state.Status,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No active search
	}
	if err != nil {
		return nil, fmt.Errorf("get active vanity search state: %w", err)
	}

	return &state, nil
}

// DeleteVanitySearchState removes a search state
func (s *SQLiteStorage) DeleteVanitySearchState(ctx context.Context, id int64) error {
	query := `DELETE FROM vanity_search_state WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete vanity search state: %w", err)
	}
	return nil
}

// UpdateVanitySearchProgress updates attempts and matches count
func (s *SQLiteStorage) UpdateVanitySearchProgress(ctx context.Context, id int64, attempts int64, matchesFound int) error {
	query := `
		UPDATE vanity_search_state
		SET attempts = ?, matches_found = ?, last_update = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, attempts, matchesFound, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update vanity search progress: %w", err)
	}
	return nil
}

// MarkVanitySearchCompleted marks a search as completed
func (s *SQLiteStorage) MarkVanitySearchCompleted(ctx context.Context, id int64) error {
	query := `
		UPDATE vanity_search_state
		SET status = 'completed', last_update = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("mark vanity search completed: %w", err)
	}
	return nil
}

// MarkVanitySearchPaused marks a search as paused
func (s *SQLiteStorage) MarkVanitySearchPaused(ctx context.Context, id int64) error {
	query := `
		UPDATE vanity_search_state
		SET status = 'paused', last_update = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("mark vanity search paused: %w", err)
	}
	return nil
}

// SerializePatterns converts patterns to JSON string
func SerializePatterns(patterns interface{}) (string, error) {
	data, err := json.Marshal(patterns)
	if err != nil {
		return "", fmt.Errorf("serialize patterns: %w", err)
	}
	return string(data), nil
}

// DeserializePatterns converts JSON string back to patterns
func DeserializePatterns(data string, patterns interface{}) error {
	if err := json.Unmarshal([]byte(data), patterns); err != nil {
		return fmt.Errorf("deserialize patterns: %w", err)
	}
	return nil
}
