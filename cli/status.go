// Package cli — status strip for home screen display.
package cli

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"evmwalletbot/config"
	"evmwalletbot/core"
)

// StatusStrip displays current system state on the home screen.
type StatusStrip struct {
	WalletCount int64
	LastRunRate int
	DBName      string
}

// GetStatusStrip retrieves current system status from the database.
func GetStatusStrip(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (*StatusStrip, error) {
	var count int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM wallets").Scan(&count)
	if err != nil {
		return nil, err
	}

	// ponytail: Simple status strip without persistent rate tracking
	// Ceiling: No rate history. Upgrade: Add simple file-based cache if needed.
	return &StatusStrip{
		WalletCount: count,
		LastRunRate: 0, // No persistent tracking yet
		DBName:      cfg.DBName,
	}, nil
}

// Render displays the status strip with color coding.
func (s *StatusStrip) Render() {
	if !core.IsColorEnabled() {
		// Fallback: plain text
		fmt.Printf("Status: %s wallets in %s\n\n", formatNumber(int(s.WalletCount)), s.DBName)
		return
	}

	status := core.Success("READY")
	walletInfo := fmt.Sprintf("%s wallets in %s",
		formatNumber(int(s.WalletCount)), s.DBName)

	lastRun := core.Hint("no recent runs")
	if s.LastRunRate > 0 {
		lastRun = fmt.Sprintf("last run %s/s", formatNumber(s.LastRunRate))
	}

	fmt.Printf("\n   %s   ·   %s   ·   %s\n\n", status, walletInfo, lastRun)
}

// formatNumber formats an integer with thousand separators.
// Uses the implementation from core package.
func formatNumber(n int) string {
	return core.FormatNumber(n)
}
