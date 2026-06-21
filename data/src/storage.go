package src

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "modernc.org/sqlite"
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


// ============================================================================
// Database Connection and Pool Management
// ============================================================================

// ponytail: Connection pool lifecycle constants (avoid import cycle with core package)
const (
	MaxConnLifetime   = 5 * time.Minute
	MaxConnIdleTime   = 2 * time.Minute
	HealthCheckPeriod = 1 * time.Minute
)

// ponytail: Compile regex once at package initialization for better performance
var dbNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// EnsureDatabase connects to the always-existing "postgres" maintenance database,
// checks whether the target database (cfg.DBName) exists, and creates it if not.
//
// This must be called BEFORE Connect() so the program never crashes on first run
// with "database does not exist".
// ponytail: Now accepts context for timeout/cancellation control
func EnsureDatabase(ctx context.Context, cfg *Config) error {
	// Connect to the built-in "postgres" system DB — it always exists.
	mainDSN := buildDSN(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, "postgres", cfg.DBSSLMode)

	pool, err := pgxpool.New(ctx, mainDSN)
	if err != nil {
		return fmt.Errorf("open maintenance connection: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("cannot reach PostgreSQL server (%s:%d): %w", cfg.DBHost, cfg.DBPort, err)
	}

	// Check whether our target database already exists.
	var exists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`,
		cfg.DBName,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}

	if exists {
		log.Printf("[INFO] Database '%s' already exists — will write wallets into it\n", cfg.DBName)
		return nil
	}

	// CREATE DATABASE must run outside any transaction block (PostgreSQL requirement).
	// ponytail: Validate database name to prevent SQL injection and ensure PostgreSQL compliance
	if err := validateDatabaseName(cfg.DBName); err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}

	log.Printf("[INFO] Database '%s' not found — creating it now...\n", cfg.DBName)
	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, cfg.DBName))
	if err != nil {
		return fmt.Errorf("create database '%s': %w", cfg.DBName, err)
	}

	log.Printf("[INFO] Database '%s' created successfully\n", cfg.DBName)
	return nil
}

// Connect opens and validates a PostgreSQL connection pool to cfg.DBName.
// Always call EnsureDatabase() first.
// ponytail: Now accepts context for timeout/cancellation control
func Connect(ctx context.Context, cfg *Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	// Pool tuning for high-throughput batch inserts.
	// ponytail: Now configurable via DB_MAX_CONNS and DB_MIN_CONNS environment variables
	poolConfig.MaxConns = int32(cfg.DBMaxConns)
	poolConfig.MinConns = int32(cfg.DBMinConns)
	poolConfig.MaxConnLifetime = MaxConnLifetime
	poolConfig.MaxConnIdleTime = MaxConnIdleTime
	poolConfig.HealthCheckPeriod = HealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping '%s': %w", cfg.DBName, err)
	}

	return pool, nil
}

// buildDSN constructs a pgx connection string.
// pgx uses standard PostgreSQL connection URIs or keyword=value format.
func buildDSN(host string, port int, user, password, dbname, sslmode string) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode,
	)
}

// validateDatabaseName ensures the database name follows PostgreSQL identifier rules.
// ponytail: Strict validation prevents edge cases and improves error messages.
// PostgreSQL identifiers: alphanumeric + underscore, max 63 chars, must start with letter/underscore.
func validateDatabaseName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("database name cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("database name too long (max 63 chars): %s", name)
	}
	// PostgreSQL identifier rules: start with letter/underscore, then alphanumeric/underscore
	if !dbNameRegex.MatchString(name) {
		return fmt.Errorf("invalid database name '%s': must start with letter/underscore and contain only letters, numbers, and underscores", name)
	}
	return nil
}

// ============================================================================
// Schema Migrations
// ============================================================================

// schema is fully idempotent; safe to run on every startup.
const schema = `
-- ─────────────────────────────────────────
--  wallets
-- ─────────────────────────────────────────
CREATE TABLE IF NOT EXISTS wallets (
    id          BIGSERIAL    PRIMARY KEY,
    address     BYTEA        NOT NULL UNIQUE,   -- 20 bytes (UNIQUE: DB enforces no duplicate addresses)
    private_key BYTEA        NOT NULL,          -- 32 bytes (collision probability: 1/2^256, essentially impossible)
    created_at  TIMESTAMPTZ  DEFAULT NOW(),
    status      SMALLINT     DEFAULT 0,         -- 0=unused, 1=used, 2=reserved
    metadata    JSONB
);

-- ponytail: Removed duplicate idx_wallets_address (UNIQUE constraint already creates index).
-- If range scans on address become common, add back with different predicate.
CREATE INDEX IF NOT EXISTS idx_wallets_status     ON wallets (status);
CREATE INDEX IF NOT EXISTS idx_wallets_created_at ON wallets (created_at DESC);

-- ─────────────────────────────────────────
--  wallet_events (partitioned by month)
-- ─────────────────────────────────────────
-- ponytail: Partitioning enables O(1) old data deletion and faster queries.
-- Ceiling: ~100M events per partition. Upgrade: add more partitions as needed.
CREATE TABLE IF NOT EXISTS wallet_events (
    id          BIGSERIAL    NOT NULL,
    wallet_id   BIGINT       NOT NULL REFERENCES wallets (id) ON DELETE CASCADE,
    event_type  VARCHAR(64)  NOT NULL,
    event_data  JSONB,
    created_at  TIMESTAMPTZ  DEFAULT NOW() NOT NULL
) PARTITION BY RANGE (created_at);

-- Create default partition for current and future months
CREATE TABLE IF NOT EXISTS wallet_events_default PARTITION OF wallet_events DEFAULT;

CREATE INDEX IF NOT EXISTS idx_events_wallet_id   ON wallet_events (wallet_id);
CREATE INDEX IF NOT EXISTS idx_events_event_type  ON wallet_events (event_type);
CREATE INDEX IF NOT EXISTS idx_events_created_at  ON wallet_events (created_at DESC);

-- ─────────────────────────────────────────
--  system_stats (cached counters)
-- ─────────────────────────────────────────
-- ponytail: O(1) stats retrieval instead of COUNT(*) on millions of rows.
-- Updated via triggers on INSERT/UPDATE/DELETE.
CREATE TABLE IF NOT EXISTS system_stats (
    id              SMALLINT     PRIMARY KEY DEFAULT 1 CHECK (id = 1), -- singleton table
    total_wallets   BIGINT       DEFAULT 0,
    unused_wallets  BIGINT       DEFAULT 0,
    used_wallets    BIGINT       DEFAULT 0,
    total_events    BIGINT       DEFAULT 0,
    last_updated    TIMESTAMPTZ  DEFAULT NOW()
);

-- Initialize stats row if not exists
INSERT INTO system_stats (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- ─────────────────────────────────────────
--  Triggers for automatic stats updates
-- ─────────────────────────────────────────
-- ponytail: Row-level trigger with delta tracking for O(1) performance at any scale.
-- Ceiling: None. Scales to billions of rows. Upgrade: none needed.
CREATE OR REPLACE FUNCTION update_wallet_stats()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- New wallet created (always starts as unused, status=0)
        UPDATE system_stats SET
            total_wallets = total_wallets + 1,
            unused_wallets = unused_wallets + 1,
            last_updated = NOW()
        WHERE id = 1;
        
    ELSIF TG_OP = 'UPDATE' THEN
        -- Status changed: adjust used/unused counters
        IF OLD.status = 0 AND NEW.status != 0 THEN
            -- Wallet became used
            UPDATE system_stats SET
                unused_wallets = unused_wallets - 1,
                used_wallets = used_wallets + 1,
                last_updated = NOW()
            WHERE id = 1;
        ELSIF OLD.status != 0 AND NEW.status = 0 THEN
            -- Wallet became unused (rare but possible)
            UPDATE system_stats SET
                unused_wallets = unused_wallets + 1,
                used_wallets = used_wallets - 1,
                last_updated = NOW()
            WHERE id = 1;
        END IF;
        
    ELSIF TG_OP = 'DELETE' THEN
        -- Wallet deleted: decrement appropriate counters
        UPDATE system_stats SET
            total_wallets = total_wallets - 1,
            unused_wallets = unused_wallets - CASE WHEN OLD.status = 0 THEN 1 ELSE 0 END,
            used_wallets = used_wallets - CASE WHEN OLD.status != 0 THEN 1 ELSE 0 END,
            last_updated = NOW()
        WHERE id = 1;
    END IF;
    
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS wallet_stats_trigger ON wallets;
CREATE TRIGGER wallet_stats_trigger
    AFTER INSERT OR UPDATE OR DELETE ON wallets
    FOR EACH ROW EXECUTE FUNCTION update_wallet_stats();

CREATE OR REPLACE FUNCTION update_event_stats()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE system_stats SET
            total_events = total_events + 1,
            last_updated = NOW()
        WHERE id = 1;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE system_stats SET
            total_events = total_events - 1,
            last_updated = NOW()
        WHERE id = 1;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS event_stats_trigger ON wallet_events;
CREATE TRIGGER event_stats_trigger
    AFTER INSERT OR DELETE ON wallet_events
    FOR EACH ROW EXECUTE FUNCTION update_event_stats();

-- ─────────────────────────────────────────
--  database_health (maintenance monitoring)
-- ─────────────────────────────────────────
CREATE TABLE IF NOT EXISTS database_health (
    id              SERIAL       PRIMARY KEY,
    table_name      VARCHAR(64)  NOT NULL,
    total_size      BIGINT       NOT NULL,
    index_size      BIGINT       NOT NULL,
    dead_tuples     BIGINT       NOT NULL,
    live_tuples     BIGINT       NOT NULL,
    last_vacuum     TIMESTAMPTZ,
    last_autovacuum TIMESTAMPTZ,
    checked_at      TIMESTAMPTZ  DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_health_checked_at ON database_health (checked_at DESC);
`

// Migrate creates all required tables and indexes if they do not already exist.
func Migrate(pool *pgxpool.Pool) error {
	ctx := context.Background()
	if _, err := pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Sync existing data to system_stats if table was just created
	if err := syncSystemStats(pool); err != nil {
		return fmt.Errorf("sync stats failed: %w", err)
	}

	return nil
}

// syncSystemStats recalculates stats from actual data (run once after migration).
func syncSystemStats(pool *pgxpool.Pool) error {
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		UPDATE system_stats SET
			total_wallets = (SELECT COUNT(*) FROM wallets),
			unused_wallets = (SELECT COUNT(*) FROM wallets WHERE status = 0),
			used_wallets = (SELECT COUNT(*) FROM wallets WHERE status != 0),
			total_events = (SELECT COUNT(*) FROM wallet_events),
			last_updated = NOW()
		WHERE id = 1
	`)
	return err
}

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


// ============================================================================
// PostgreSQL Storage Implementation
// ============================================================================

// PostgresStorage implements the Storage interface using PostgreSQL.
type PostgresStorage struct {
	pool *pgxpool.Pool
}

// NewPostgresStorage creates a new PostgreSQL storage backend.
// Returns an error if the database is unreachable.
func NewPostgresStorage(ctx context.Context, cfg *Config) (*PostgresStorage, error) {
	// Ensure database exists
	if err := EnsureDatabase(ctx, cfg); err != nil {
		return nil, fmt.Errorf("ensure database: %w", err)
	}

	// Connect to database
	pool, err := Connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	return &PostgresStorage{
		pool: pool,
	}, nil
}

// Migrate runs schema migrations to create tables and indexes.
func (p *PostgresStorage) Migrate(ctx context.Context) error {
	return Migrate(p.pool)
}

// SaveWallets persists a batch of wallets and returns their assigned IDs.
// Uses PostgreSQL COPY protocol for high-performance bulk inserts.
func (p *PostgresStorage) SaveWallets(ctx context.Context, wallets []*Wallet) ([]int64, error) {
	if len(wallets) == 0 {
		return nil, nil
	}

	// Use existing insertWalletBatchCopy from database package
	// We need to expose this function or duplicate the logic here
	// For now, we'll use a simpler multi-row INSERT
	return p.insertWalletBatch(ctx, wallets)
}

// insertWalletBatch inserts a batch of wallets using multi-row INSERT.
func (p *PostgresStorage) insertWalletBatch(ctx context.Context, wallets []*Wallet) ([]int64, error) {
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
func (p *PostgresStorage) GetWalletByID(ctx context.Context, id int64) (*WalletRecord, error) {
	var record WalletRecord

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
func (p *PostgresStorage) GetWalletByAddress(ctx context.Context, address []byte) (*WalletRecord, error) {
	var record WalletRecord

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
func (p *PostgresStorage) GetStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{}

	err := p.pool.QueryRow(ctx, `
		SELECT 
			total_wallets,
			unused_wallets,
			used_wallets,
			total_events
		FROM system_stats
		WHERE id = 1
	`).Scan(&stats.TotalWallets, &stats.UnusedWallets, &stats.UsedWallets, &stats.TotalEvents)
	if err != nil {
		return nil, fmt.Errorf("query cached stats: %w", err)
	}

	if stats.TotalWallets == 0 {
		err = p.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&stats.DBSizeBytes)
		if err != nil {
			return nil, fmt.Errorf("query db size: %w", err)
		}
		return stats, nil
	}

	err = p.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM wallets WHERE created_at >= CURRENT_DATE
	`).Scan(&stats.WalletsToday)
	if err != nil {
		return nil, fmt.Errorf("count today's wallets: %w", err)
	}

	err = p.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&stats.DBSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("query db size: %w", err)
	}

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
func (p *PostgresStorage) GetPoolStats() *PoolStats {
	stats := p.pool.Stat()
	return &PoolStats{
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

