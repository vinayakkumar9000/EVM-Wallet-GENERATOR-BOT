// Package core — statistics queries and display.
package core

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

// GetStats queries statistics using cached counters for O(1) performance.
// ponytail: Replaced COUNT(*) with cached system_stats table (instant vs seconds).
// Ceiling: Stats lag by ~1ms (trigger execution time). Upgrade: none needed.
func GetStats(ctx context.Context, pool *pgxpool.Pool) (*Stats, error) {
	s := &Stats{}

	// O(1) lookup from cached stats table (updated via triggers)
	err := pool.QueryRow(ctx, `
		SELECT 
			total_wallets,
			unused_wallets,
			used_wallets,
			total_events
		FROM system_stats
		WHERE id = 1
	`).Scan(&s.TotalWallets, &s.UnusedWallets, &s.UsedWallets, &s.TotalEvents)
	if err != nil {
		return nil, fmt.Errorf("query cached stats: %w", err)
	}

	// Wallets created today (still needs COUNT but only scans today's partition)
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM wallets WHERE created_at >= CURRENT_DATE
	`).Scan(&s.WalletsToday)
	if err != nil {
		return nil, fmt.Errorf("query today's wallets: %w", err)
	}

	// Database size (fast metadata query)
	err = pool.QueryRow(ctx, `
		SELECT pg_database_size(current_database())
	`).Scan(&s.DBSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("query db size: %w", err)
	}

	// Last created wallet (index scan, very fast)
	var lastCreated time.Time
	err = pool.QueryRow(ctx, `
		SELECT created_at FROM wallets ORDER BY id DESC LIMIT 1
	`).Scan(&lastCreated)
	if err == nil {
		s.LastCreatedAt = &lastCreated
	}

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
