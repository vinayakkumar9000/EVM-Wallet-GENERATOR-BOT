// Package cli — status strip display for home screen.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	"evmwalletbot/config"
	"evmwalletbot/core"
)

// printStatusStrip displays current system status at the top of the home screen.
func printStatusStrip(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
	// Get wallet count
	var walletCount int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallets`).Scan(&walletCount)
	if err != nil {
		walletCount = 0
	}

	// Get output directory (absolute path, shortened for display)
	outputDir := cfg.OutputDir
	if absPath, err := filepath.Abs(outputDir); err == nil {
		// Show relative to home or current dir for brevity
		if home, err := os.UserHomeDir(); err == nil {
			if rel, err := filepath.Rel(home, absPath); err == nil && len(rel) < len(absPath) {
				outputDir = "~/" + filepath.ToSlash(rel)
			}
		}
	}

	// Format status line
	status := core.Success("READY")
	walletInfo := fmt.Sprintf("%s wallets in %s", 
		core.Highlight(formatNumber(walletCount)), 
		core.Info(outputDir))
	
	// Display status strip
	fmt.Printf("\n   %s  ·  %s\n\n", status, walletInfo)
}

// formatNumber formats a number with thousand separators.
func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	millions := n / 1000000
	thousands := (n % 1000000) / 1000
	ones := n % 1000
	return fmt.Sprintf("%d,%03d,%03d", millions, thousands, ones)
}
