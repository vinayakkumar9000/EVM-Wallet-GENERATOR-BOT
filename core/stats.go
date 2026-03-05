// Package core — statistics queries and display.
package core

import (
	"database/sql"
	"fmt"
	"time"
)

// Stats aggregates wallet and event counters for the stats display.
type Stats struct {
	TotalWallets  int64
	WalletsToday  int64
	UnusedWallets int64
	UsedWallets   int64
	TotalEvents   int64
	DBSizeBytes   int64
	LastCreatedAt *time.Time
}

// GetStats queries all statistics inside a single READ ONLY transaction so
// every counter comes from the same database snapshot.
//
// BUG FIX #6 — without a transaction, 6 serial queries against a live table
// could return inconsistent values (e.g. TotalWallets counted before a batch
// finishes, UnusedWallets counted after, making the numbers not add up).
func GetStats(db *sql.DB) (*Stats, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin stats tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Read-only transaction — we never write here.
	if _, err := tx.Exec(`SET TRANSACTION READ ONLY`); err != nil {
		return nil, fmt.Errorf("set read only: %w", err)
	}

	s := &Stats{}

	type scanPair struct {
		dest  interface{}
		query string
	}

	pairs := []scanPair{
		{&s.TotalWallets, `SELECT COUNT(*) FROM wallets`},
		{&s.WalletsToday, `SELECT COUNT(*) FROM wallets WHERE created_at >= CURRENT_DATE`},
		{&s.UnusedWallets, `SELECT COUNT(*) FROM wallets WHERE status = 0`},
		{&s.UsedWallets, `SELECT COUNT(*) FROM wallets WHERE status != 0`},
		{&s.TotalEvents, `SELECT COUNT(*) FROM wallet_events`},
		{&s.DBSizeBytes, `SELECT pg_database_size(current_database())`},
	}

	for _, p := range pairs {
		if err := tx.QueryRow(p.query).Scan(p.dest); err != nil {
			return nil, fmt.Errorf("stats query failed (%s): %w", p.query, err)
		}
	}

	var lastCreated time.Time
	err = tx.QueryRow(
		`SELECT created_at FROM wallets ORDER BY id DESC LIMIT 1`,
	).Scan(&lastCreated)
	if err == nil {
		s.LastCreatedAt = &lastCreated
	}

	// Commit is a no-op for a read-only tx but is idiomatic.
	_ = tx.Commit()
	return s, nil
}

// PrintStats renders the statistics table to stdout.
func PrintStats(s *Stats) {
	line := "  ├─────────────────────────────────────────────────┤"
	top := "  ╔═════════════════════════════════════════════════╗"
	bot := "  ╚═════════════════════════════════════════════════╝"
	title := "  ║              WALLET STATISTICS                 ║"

	fmt.Println()
	fmt.Println(top)
	fmt.Println(title)
	fmt.Println(line)
	printRow("Total wallets", fmt.Sprintf("%d", s.TotalWallets))
	printRow("Wallets created today", fmt.Sprintf("%d", s.WalletsToday))
	printRow("Unused wallets", fmt.Sprintf("%d", s.UnusedWallets))
	printRow("Used wallets", fmt.Sprintf("%d", s.UsedWallets))
	fmt.Println(line)
	printRow("Total events logged", fmt.Sprintf("%d", s.TotalEvents))
	printRow("Database size", formatBytes(s.DBSizeBytes))
	fmt.Println(line)

	if s.LastCreatedAt != nil {
		printRow("Last wallet created", s.LastCreatedAt.Format("2006-01-02 15:04:05"))
	} else {
		printRow("Last wallet created", "N/A — no wallets yet")
	}

	fmt.Println(bot)
	fmt.Println()
}

func printRow(label, value string) {
	fmt.Printf("  ║  %-26s : %-20s ║\n", label, value)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
