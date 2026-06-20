package main

import (
	"context"
	"flag"
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

// ponytail: Application-level constants (avoid import cycle with core package)
const (
	ShutdownGracePeriod = 2 * time.Second
	PoolMonitorInterval = 30 * time.Second
)

func main() {
	// ── Command-line flags for non-interactive mode ───────────────────────
	var (
		generateCount = flag.Int("count", 0, "Generate N wallets and exit (non-interactive mode)")
		exportMode    = flag.String("export-mode", "", "Export mode: paired, key-only, address-only, combined")
		exportDir     = flag.String("export-dir", "", "Export directory path")
		storageType   = flag.String("storage", "", "Storage backend: sqlite (default) or postgres")
		dataDir       = flag.String("data-dir", "", "Data directory for SQLite (auto-determined if empty)")
		showVersion   = flag.Bool("version", false, "Show version and exit")
		showHelp      = flag.Bool("help", false, "Show help and exit")
	)
	flag.Parse()

	// ── Show version ──────────────────────────────────────────────────────
	if *showVersion {
		fmt.Println("evmwalletbot v1.0.0")
		os.Exit(0)
	}

	// ── Show help ─────────────────────────────────────────────────────────
	if *showHelp {
		fmt.Println("EVM Wallet Generator - Generate Ethereum-compatible wallets")
		fmt.Println("\nUsage:")
		fmt.Println("  evmwalletbot [flags]")
		fmt.Println("\nFlags:")
		flag.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # Interactive mode (default)")
		fmt.Println("  evmwalletbot")
		fmt.Println("\n  # Generate 1000 wallets non-interactively")
		fmt.Println("  evmwalletbot -count 1000")
		fmt.Println("\n  # Generate and export to CSV")
		fmt.Println("  evmwalletbot -count 1000 -export-mode combined -export-dir ./output")
		fmt.Println("\n  # Use PostgreSQL storage (requires PostgreSQL server)")
		fmt.Println("  evmwalletbot -storage postgres")
		fmt.Println("\n  # Use custom data directory for SQLite")
		fmt.Println("  evmwalletbot -storage sqlite -data-dir ./my-wallets")
		os.Exit(0)
	}

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

	// ── Override config with CLI flags ────────────────────────────────────
	if *exportMode != "" {
		cfg.ExportEnabled = true
		cfg.ExportMode = *exportMode
	}
	if *exportDir != "" {
		cfg.ExportDir = *exportDir
	}
	if *storageType != "" {
		cfg.StorageType = *storageType
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
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
		cancel()                        // Cancel context to stop all operations
		time.Sleep(ShutdownGracePeriod) // Grace period for operations to complete
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

	// ── Ensure database exists before connecting ──────────────────────────
	log.Println("[INFO] Ensuring database exists...")
	if err := database.EnsureDatabase(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Database setup failed: %v\n", err)
		os.Exit(1)
	}

	// ── Connect to PostgreSQL ─────────────────────────────────────────────
	log.Println("[INFO] Connecting to database...")
	pool, err := database.Connect(ctx, cfg)
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
	// ponytail: Configurable monitoring interval and warning threshold
	if cfg.PoolMonitorInterval > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.PoolMonitorInterval) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					stats := pool.Stat()
					// Only log routine pool stats if logging is enabled
					if cfg.EnableLogging {
						log.Printf("[POOL] Connections: Total=%d Idle=%d Acquired=%d MaxLifetime=%v",
							stats.TotalConns(), stats.IdleConns(), stats.AcquiredConns(),
							stats.MaxLifetimeDestroyCount())
					}

					// Always warn if pool is near exhaustion (important for operations)
					threshold := cfg.PoolWarningThreshold
					if threshold <= 0 || threshold > 1.0 {
						threshold = 0.8 // Fallback to default
					}
					if stats.AcquiredConns() > int32(float64(stats.MaxConns())*threshold) {
						log.Printf("[WARN] Connection pool usage high: %d/%d (%.0f%%)",
							stats.AcquiredConns(), stats.MaxConns(),
							float64(stats.AcquiredConns())/float64(stats.MaxConns())*100)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// ── Auto-migrate schema (idempotent — safe on every run) ──────────────
	log.Println("[INFO] Verifying database schema...")
	if err := database.Migrate(pool); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Schema migration failed: %v\n", err)
		os.Exit(1)
	}
	log.Println("[INFO] Schema ready")

	// ── Auto-display stats when wallets already exist ─────────────────────
	s, err := core.GetStats(ctx, pool)
	if err == nil && s.TotalWallets > 0 {
		log.Printf("[INFO] Existing database found — %d wallets loaded\n", s.TotalWallets)
		core.PrintStats(s)
	}

	// ── Non-interactive mode: generate and exit ───────────────────────────
	if *generateCount > 0 {
		log.Printf("[INFO] Non-interactive mode: generating %d wallets", *generateCount)
		if err := core.GenerateWallets(ctx, pool, cfg, *generateCount); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Generation failed: %v\n", err)
			os.Exit(1)
		}
		log.Println("[INFO] Generation complete, exiting")
		os.Exit(0)
	}

	// ── Launch interactive CLI ────────────────────────────────────────────
	cli.Run(ctx, pool, cfg)
}
