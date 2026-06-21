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

	"evmwalletbot/internal"
)

const (
	ShutdownGracePeriod = 2 * time.Second
)

func main() {
	var (
		generateCount = flag.Int("count", 0, "Generate N wallets and exit (non-interactive mode)")
		exportMode    = flag.String("export-mode", "", "Export mode: paired, key-only, address-only, combined")
		exportDir     = flag.String("export-dir", "", "Export directory path")
		storageType   = flag.String("storage", "", "Storage backend: sqlite (default) or postgres")
		dataDir       = flag.String("data-dir", "", "Data directory for SQLite (auto-determined if empty)")
		verifyFile    = flag.String("verify", "", "Verify exported wallet file (txt, csv, or json)")
		showVersion   = flag.Bool("version", false, "Show version and exit")
		showHelp      = flag.Bool("help", false, "Show help and exit")
	)
	flag.Parse()

	// Handle --verify subcommand (optional, off-by-default verification)
	if *verifyFile != "" {
		if err := src.VerifyExportedFile(*verifyFile); err != nil {
			fmt.Fprintf(os.Stderr, "Verification failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("EVM Wallet Generator\n")
		fmt.Printf("Version: %s\n", src.Version)
		fmt.Printf("Commit: %s\n", src.GitCommit)
		fmt.Printf("Build Date: %s\n", src.BuildDate)
		os.Exit(0)
	}

	if *showHelp {
		fmt.Println("EVM Wallet Generator - Generate Ethereum-compatible wallets")
		fmt.Println("\nUsage:")
		fmt.Println("  evmwalletbot [flags]")
		fmt.Println("\nFlags:")
		flag.PrintDefaults()
		fmt.Println("\nExamples:")
		fmt.Println("  # Interactive mode (default, zero setup)")
		fmt.Println("  evmwalletbot")
		fmt.Println("\n  # Generate 1000 wallets non-interactively")
		fmt.Println("  evmwalletbot -count 1000")
		fmt.Println("\n  # Generate and export to paired text files")
		fmt.Println("  evmwalletbot -count 1000 -export-mode paired -export-dir ./output")
		fmt.Println("\n  # Use PostgreSQL storage (requires PostgreSQL server)")
		fmt.Println("  evmwalletbot -storage postgres")
		fmt.Println("\n  # Use custom data directory for SQLite")
		fmt.Println("  evmwalletbot -storage sqlite -data-dir ./my-wallets")
		os.Exit(0)
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("\n[FATAL] Application panic: %v", r)
			log.Println("[FATAL] Stack trace available in logs")
			os.Exit(1)
		}
	}()

	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	cfg, err := src.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Config: %v\n", err)
		os.Exit(1)
	}

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	shutdownComplete := make(chan struct{})

	go func() {
		sig := <-sigCh
		log.Printf("\n[INFO] Received signal %v, initiating graceful shutdown...", sig)
		cancel()
		time.Sleep(ShutdownGracePeriod)
		log.Println("[INFO] Shutdown complete")
		close(shutdownComplete)
	}()

	defer func() {
		select {
		case <-shutdownComplete:
		default:
		}
	}()

	store, err := src.NewStorage(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Storage setup failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		log.Println("[INFO] Closing storage...")
		if err := store.Close(); err != nil {
			log.Printf("[WARN] Storage close: %v", err)
		}
	}()

	log.Println("[INFO] Verifying storage schema...")
	if err := store.Migrate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Schema migration failed: %v\n", err)
		os.Exit(1)
	}
	log.Printf("[INFO] Schema ready (%s backend)", store.StorageType())

	if store.StorageType() == "postgres" && cfg.PoolMonitorInterval > 0 {
		startPoolMonitor(ctx, store, cfg)
	}

	s, err := src.GetStats(ctx, store)
	if err == nil && s.TotalWallets > 0 {
		log.Printf("[INFO] Existing data found — %d wallets loaded\n", s.TotalWallets)
		src.PrintStats(s)
	}

	if *generateCount > 0 {
		log.Printf("[INFO] Non-interactive mode: generating %d wallets", *generateCount)
		if err := src.GenerateWallets(ctx, store, cfg, *generateCount); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Generation failed: %v\n", err)
			os.Exit(1)
		}
		log.Println("[INFO] Generation complete, exiting")
		os.Exit(0)
	}

	src.Run(ctx, store, cfg)
}

func startPoolMonitor(ctx context.Context, store src.Storage, cfg *src.Config) {
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.PoolMonitorInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				stats := store.GetPoolStats()
				if stats == nil {
					continue
				}
				if cfg.EnableLogging {
					log.Printf("[POOL] Connections: Total=%d Idle=%d Acquired=%d Max=%d",
						stats.TotalConns, stats.IdleConns, stats.AcquiredConns, stats.MaxConns)
				}
				threshold := cfg.PoolWarningThreshold
				if threshold <= 0 || threshold > 1.0 {
					threshold = 0.8
				}
				if stats.Usage() > threshold {
					log.Printf("[WARN] Connection pool usage high: %d/%d (%.0f%%)",
						stats.AcquiredConns, stats.MaxConns, stats.Usage()*100)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
