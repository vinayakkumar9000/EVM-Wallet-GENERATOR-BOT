package internal

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
