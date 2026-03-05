// Package database — automatic schema migrations.
package database

import (
	"database/sql"
	"fmt"
)

// schema is fully idempotent; safe to run on every startup.
const schema = `
-- ─────────────────────────────────────────
--  wallets
-- ─────────────────────────────────────────
CREATE TABLE IF NOT EXISTS wallets (
    id          BIGSERIAL    PRIMARY KEY,
    address     BYTEA        NOT NULL UNIQUE,   -- 20 bytes (UNIQUE: DB enforces no duplicate addresses)
    private_key BYTEA        NOT NULL UNIQUE,   -- 32 bytes (UNIQUE: DB enforces no duplicate keys)
    created_at  TIMESTAMPTZ  DEFAULT NOW(),
    status      SMALLINT     DEFAULT 0,         -- 0=unused, 1=used, 2=reserved
    metadata    JSONB
);

-- BUG FIX #5: address already has UNIQUE above (which implicitly creates an index).
-- The explicit index below is kept for query-planner hints on non-equality scans.
CREATE INDEX IF NOT EXISTS idx_wallets_address    ON wallets (address);
CREATE INDEX IF NOT EXISTS idx_wallets_status     ON wallets (status);
CREATE INDEX IF NOT EXISTS idx_wallets_created_at ON wallets (created_at DESC);

-- ─────────────────────────────────────────
--  wallet_events
-- ─────────────────────────────────────────
CREATE TABLE IF NOT EXISTS wallet_events (
    id          BIGSERIAL    PRIMARY KEY,
    wallet_id   BIGINT       NOT NULL REFERENCES wallets (id) ON DELETE CASCADE,
    event_type  VARCHAR(64)  NOT NULL,
    event_data  JSONB,
    created_at  TIMESTAMPTZ  DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_wallet_id   ON wallet_events (wallet_id);
CREATE INDEX IF NOT EXISTS idx_events_event_type  ON wallet_events (event_type);
CREATE INDEX IF NOT EXISTS idx_events_created_at  ON wallet_events (created_at DESC);
`

// Migrate creates all required tables and indexes if they do not already exist.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	return nil
}
