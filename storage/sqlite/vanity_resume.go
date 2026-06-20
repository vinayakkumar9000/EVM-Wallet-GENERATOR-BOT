// Package sqlite — vanity search resume functionality
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

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
