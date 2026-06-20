// Package core — statistics queries and display.
package core

import (
	"context"
	"fmt"
	"time"

	"evmwalletbot/storage"
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

// GetStats queries statistics from the active storage backend.
func GetStats(ctx context.Context, store storage.Storage) (*Stats, error) {
	base, err := store.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	s := &Stats{
		TotalWallets:  base.TotalWallets,
		WalletsToday:  base.WalletsToday,
		UnusedWallets: base.UnusedWallets,
		UsedWallets:   base.UsedWallets,
		TotalEvents:   base.TotalEvents,
		DBSizeBytes:   base.DBSizeBytes,
	}

	if !base.NewestWallet.IsZero() {
		t := base.NewestWallet
		s.LastCreatedAt = &t
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
	printRow("Database size", FormatBytes(s.DBSizeBytes))
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

// FormatBytes renders a byte count in human-readable form.
func FormatBytes(b int64) string {
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
