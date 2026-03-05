package main

import (
	"fmt"
	"log"
	"os"

	"evmwalletbot/cli"
	"evmwalletbot/config"
	"evmwalletbot/core"
	"evmwalletbot/database"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	// ── Load configuration ────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Config: %v\n", err)
		os.Exit(1)
	}

	// ── Connect to PostgreSQL ─────────────────────────────────────────────
	log.Println("[INFO] Connecting to database...")
	db, err := database.Connect(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[ERROR] Cannot connect to PostgreSQL: %v\n"+
				"        → Check DB_HOST / DB_PORT / DB_USER / DB_PASSWORD in .env\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── Auto-migrate schema (idempotent — safe on every run) ──────────────
	// CREATE TABLE IF NOT EXISTS — never drops or truncates existing wallets.
	// New wallets are always INSERTed (appended), never overwritten.
	log.Println("[INFO] Verifying database schema...")
	if err := database.Migrate(db); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Schema migration failed: %v\n", err)
		os.Exit(1)
	}
	log.Println("[INFO] Schema ready")

	// ── BUG FIX #3 — auto-display stats when wallets already exist ────────
	// The spec states: "If wallets already exist, load statistics automatically."
	// Previously the program jumped straight to the menu with no context.
	s, err := core.GetStats(db)
	if err == nil && s.TotalWallets > 0 {
		log.Printf("[INFO] Existing database found — %d wallets loaded\n", s.TotalWallets)
		core.PrintStats(s)
	}

	// ── Launch interactive CLI ────────────────────────────────────────────
	cli.Run(db, cfg)
}
