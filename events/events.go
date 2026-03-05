// Package events handles structured event logging into wallet_events table.
package events

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"
)

// EventType is a string enum for event classification.
type EventType string

const (
	WalletCreated    EventType = "wallet_created"
	BalanceReceived  EventType = "balance_received"
	TransactionSent  EventType = "transaction_sent"
	RotationComplete EventType = "rotation_complete"
	FaucetClaim      EventType = "faucet_claim"
	BalanceUpdated   EventType = "balance_updated"
)

// RecentEvent is used for display queries.
type RecentEvent struct {
	ID        int64
	WalletID  int64
	EventType string
	EventData string
	CreatedAt string
}

// Log inserts a single event for one wallet.
func Log(db *sql.DB, walletID int64, eventType EventType, data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}
	_, err = db.Exec(
		`INSERT INTO wallet_events (wallet_id, event_type, event_data) VALUES ($1, $2, $3)`,
		walletID, string(eventType), jsonData,
	)
	return err
}

// LogBatch inserts one event row per wallet using PostgreSQL unnest().
//
// BUG FIX #2 — the original version built N×3 parameters (walletID, eventType,
// jsonData per row).  For a 500-wallet batch that was 1500 parameters with the
// same eventType and jsonData string repeated 500 times — wasted memory and
// query parse overhead.
//
// The unnest approach uses exactly 3 parameters regardless of batch size:
//   $1 = bigint[]   — array of wallet IDs (pq.Array handles the cast)
//   $2 = text       — event type (same for all rows)
//   $3 = jsonb text — event data (same for all rows)
//
// BUG FIX #6 — wrapped in an explicit transaction so a partial failure rolls
// back the entire batch instead of leaving orphaned event rows.
func LogBatch(db *sql.DB, walletIDs []int64, eventType EventType, data map[string]interface{}) error {
	if len(walletIDs) == 0 {
		return nil
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin event tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO wallet_events (wallet_id, event_type, event_data)
		SELECT unnest($1::bigint[]), $2, $3::jsonb
	`, pq.Array(walletIDs), string(eventType), string(jsonData))

	if err != nil {
		return fmt.Errorf("batch event insert: %w", err)
	}

	return tx.Commit()
}

// GetRecent returns the last `limit` events ordered newest-first.
func GetRecent(db *sql.DB, limit int) ([]RecentEvent, error) {
	rows, err := db.Query(`
		SELECT
			e.id,
			e.wallet_id,
			e.event_type,
			COALESCE(e.event_data::text, '{}'),
			to_char(e.created_at, 'YYYY-MM-DD HH24:MI:SS')
		FROM wallet_events e
		ORDER BY e.id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []RecentEvent
	for rows.Next() {
		var ev RecentEvent
		if err := rows.Scan(&ev.ID, &ev.WalletID, &ev.EventType, &ev.EventData, &ev.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}
