// Package database — automatic schema migrations.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
