package main

import (
	"evmwalletbot/config"
	"evmwalletbot/core"
	"fmt"
)

func main() {
	cfg := &config.Config{
		BatchSize: 500,
		Workers:   16,
	}

	title := core.Highlight("MAIN MENU")
	genHint := core.Hint("batch %d", cfg.BatchSize)
	settingsHint := core.Hint("%d workers", cfg.Workers)

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │   %s                                        │
  ├──────────────────────────────────────────────────────┤
  │   %s %s   Generate wallets               %s │
  │   %s %s   Vanity generation                          │
  │   %s %s   Statistics                                 │
  │   %s %s   Configuration / tuning         %s │
  │   %s %s   Benchmark / tuning                         │
  │   %s %s   Import & verify key                        │
  │   %s %s   Help                                       │
  │   %s %s   Exit                                       │
  └──────────────────────────────────────────────────────┘
  %s `,
		title,
		core.Success("1"), core.Hint("[G]"), genHint,
		core.Success("2"), core.Hint("[V]"),
		core.Info("3"), core.Hint("[S]"),
		core.Info("4"), core.Hint("[C]"), settingsHint,
		core.Info("5"), core.Hint("[B]"),
		core.Info("6"), core.Hint("[I]"),
		core.Info("7"), core.Hint("[H]"),
		core.Warning("0"), core.Hint("[Q]"),
		core.Hint("Select option:"))

	fmt.Println("\n\nMenu printed successfully!")
}
