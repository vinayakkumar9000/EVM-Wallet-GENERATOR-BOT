// Package cli — status strip for home screen display.
package cli

import (
	"context"
	"fmt"

	"evmwalletbot/config"
	"evmwalletbot/core"
	"evmwalletbot/storage"
)

// StatusStrip displays current system state on the home screen.
type StatusStrip struct {
	WalletCount int64
	LastRunRate int
	StorageInfo string
}

// GetStatusStrip retrieves current system status from storage.
func GetStatusStrip(ctx context.Context, store storage.Storage, cfg *config.Config) (*StatusStrip, error) {
	count, err := store.CountWallets(ctx)
	if err != nil {
		return nil, err
	}

	label := store.StorageType()
	if cfg.StorageType == "postgres" {
		label = fmt.Sprintf("postgres (%s)", cfg.DBName)
	} else if cfg.DataDir != "" {
		label = fmt.Sprintf("sqlite (%s)", cfg.DataDir)
	}

	return &StatusStrip{
		WalletCount: count,
		LastRunRate: 0,
		StorageInfo: label,
	}, nil
}

// Render displays the status strip with color coding.
func (s *StatusStrip) Render() {
	if !core.IsColorEnabled() {
		fmt.Printf("Status: %s wallets in %s\n\n", formatNumber(int(s.WalletCount)), s.StorageInfo)
		return
	}

	status := core.Success("READY")
	walletInfo := fmt.Sprintf("%s wallets in %s",
		formatNumber(int(s.WalletCount)), s.StorageInfo)

	lastRun := core.Hint("no recent runs")
	if s.LastRunRate > 0 {
		lastRun = fmt.Sprintf("last run %s/s", formatNumber(s.LastRunRate))
	}

	fmt.Printf("\n   %s   ·   %s   ·   %s\n\n", status, walletInfo, lastRun)
}

func formatNumber(n int) string {
	return core.FormatNumber(n)
}
