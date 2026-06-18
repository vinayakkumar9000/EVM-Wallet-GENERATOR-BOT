// Package events handles structured event logging into wallet_events table.
package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EventType is a string enum for event classification.
type EventType string

const (
	WalletCreated EventType = "wallet_created"
	// Removed unused event types: BalanceReceived, TransactionSent, 
	// RotationComplete, FaucetClaim, BalanceUpdated
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
func Log(ctx context.Context, pool *pgxpool.Pool, walletID int64, eventType EventType, data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}
	
	_, err = pool.Exec(ctx,
		`INSERT INTO wallet_events (wallet_id, event_type, event_data) VALUES ($1, $2, $3)`,
		walletID, string(eventType), jsonData,
	)
	return err
}

// LogBatch inserts one event row per wallet using PostgreSQL unnest().
// Uses exactly 3 parameters regardless of batch size for efficiency.
// ponytail: Removed transaction wrapper - single INSERT doesn't need it
func LogBatch(ctx context.Context, pool *pgxpool.Pool, walletIDs []int64, eventType EventType, data map[string]interface{}) error {
	if len(walletIDs) == 0 {
		return nil
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO wallet_events (wallet_id, event_type, event_data)
		SELECT unnest($1::bigint[]), $2, $3::jsonb
	`, walletIDs, string(eventType), string(jsonData))

	if err != nil {
		return fmt.Errorf("batch event insert: %w", err)
	}

	return nil
}

// GetRecent returns the last `limit` events ordered newest-first.
func GetRecent(ctx context.Context, pool *pgxpool.Pool, limit int) ([]RecentEvent, error) {
	rows, err := pool.Query(ctx, `
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