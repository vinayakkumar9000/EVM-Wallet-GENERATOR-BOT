package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"evmwalletbot/cli"
	"evmwalletbot/config"
	"evmwalletbot/core"
	"evmwalletbot/database"
)

func main() {
	// ── Top-level panic recovery ──────────────────────────────────────────
	defer func() {
		if r := recover(); r != nil {
			log.Printf("\n[FATAL] Application panic: %v", r)
			log.Println("[FATAL] Stack trace available in logs")
			os.Exit(1)
		}
	}()

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

	// ── Create context for graceful shutdown ──────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Setup signal handling for graceful shutdown ───────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	shutdownComplete := make(chan struct{})
	
	go func() {
		sig := <-sigCh
		log.Printf("\n[INFO] Received signal %v, initiating graceful shutdown...", sig)
		cancel() // Cancel context to stop all operations
		time.Sleep(2 * time.Second) // Grace period for operations to complete
		log.Println("[INFO] Shutdown complete")
		close(shutdownComplete)
	}()
	
	defer func() {
		select {
		case <-shutdownComplete:
			// Shutdown already handled by signal
		default:
			// Normal exit
		}
	}()

	// ── Connect to PostgreSQL ─────────────────────────────────────────────
	log.Println("[INFO] Connecting to database...")
	pool, err := database.Connect(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[ERROR] Cannot connect to PostgreSQL: %v\n"+
				"        → Check DB_HOST / DB_PORT / DB_USER / DB_PASSWORD in .env\n", err)
		os.Exit(1)
	}
	defer func() {
		log.Println("[INFO] Closing database connection...")
		pool.Close()
	}()

	// ── Start connection pool monitoring ──────────────────────────────────
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				stats := pool.Stat()
				log.Printf("[POOL] Connections: Total=%d Idle=%d Acquired=%d MaxLifetime=%v",
					stats.TotalConns(), stats.IdleConns(), stats.AcquiredConns(),
					stats.MaxLifetimeDestroyCount())
				
				// Warn if pool is near exhaustion
				if stats.AcquiredConns() > int32(float64(stats.MaxConns())*0.8) {
					log.Printf("[WARN] Connection pool usage high: %d/%d (%.0f%%)",
						stats.AcquiredConns(), stats.MaxConns(),
						float64(stats.AcquiredConns())/float64(stats.MaxConns())*100)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

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
	cli.Run(ctx, pool, cfg)
}