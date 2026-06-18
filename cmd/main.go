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

	// ── Ensure database exists before connecting ──────────────────────────
	log.Println("[INFO] Ensuring database exists...")
	if err := database.EnsureDatabase(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Database setup failed: %v\n", err)
		os.Exit(1)
	}

	// ── Connect to PostgreSQL ─────────────────────────────────────────────
	log.Println("[INFO] Connecting to database...")
	pool, err := database.Connect(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[ERROR] Cannot connect to PostgreSQL: %v\n"+
				"        → Check DB_HOST / DB_PORT / DB_USER / DB_PASSWORD in .env\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	// ── Auto-migrate schema (idempotent — safe on every run) ──────────────
	log.Println("[INFO] Verifying database schema...")
	if err := database.Migrate(pool); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Schema migration failed: %v\n", err)
		os.Exit(1)
	}
	log.Println("[INFO] Schema ready")

	// ── Auto-display stats when wallets already exist ─────────────────────
	s, err := core.GetStats(pool)
	if err == nil && s.TotalWallets > 0 {
		log.Printf("[INFO] Existing database found — %d wallets loaded\n", s.TotalWallets)
		core.PrintStats(s)
	}

	// ── Launch interactive CLI ────────────────────────────────────────────
	cli.Run(pool, cfg)
}