// Package cli implements the interactive command-line menu.
package cli

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"evmwalletbot/config"
	"evmwalletbot/core"
	"evmwalletbot/events"
)

// Run is the main entry point for the interactive CLI.
func Run(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
	reader := bufio.NewReader(os.Stdin)
	printBanner()

	for {
		// Check if context is cancelled (graceful shutdown)
		select {
		case <-ctx.Done():
			fmt.Println("\n[INFO] Shutdown requested, exiting...")
			return
		default:
		}

		printMenu()
		choice := strings.TrimSpace(readLine(reader))

		switch choice {
		case "1":
			handleGenerateMenu(ctx, pool, cfg, reader)
		case "2":
			handleStatsMenu(ctx, pool)
		case "3":
			handleLookupMenu(ctx, pool, reader)
		case "4":
			handleDatabaseMenu(ctx, pool)
		case "5":
			handleMonitoringMenu(ctx, pool)
		case "6":
			handleBenchmarkMenu()
		case "7":
			handleConfigMenu(cfg, reader)
		case "8":
			handleHelpMenu()
		case "9":
			fmt.Println("\n[INFO] Goodbye.")
			return
		default:
			fmt.Println("\n[WARN] Invalid option — please choose 1 to 9.")
		}
	}
}

// ─── Menu handlers ────────────────────────────────────────────────────────────

func handleGenerateMenu(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, reader *bufio.Reader) {
	handleGenerate(ctx, pool, cfg, reader)
}

func handleStatsMenu(ctx context.Context, pool *pgxpool.Pool) {
	handleStats(ctx, pool)
}

func handleLookupMenu(ctx context.Context, pool *pgxpool.Pool, reader *bufio.Reader) {
	handleWalletInfo(ctx, pool, reader)
}

func handleDatabaseMenu(ctx context.Context, pool *pgxpool.Pool) {
	handleDatabaseHealth(ctx, pool)
}

func handleMonitoringMenu(ctx context.Context, pool *pgxpool.Pool) {
	fmt.Println("\n[INFO] Loading recent events...")

	recent, err := events.GetRecent(ctx, pool, 10)
	if err != nil {
		fmt.Printf("[ERROR] Could not load recent events: %v\n", err)
		return
	}
	if len(recent) == 0 {
		fmt.Println("[INFO] No recent events found.")
		return
	}

	for _, ev := range recent {
		fmt.Printf("  #%d wallet=%d type=%s at=%s data=%s\n",
			ev.ID, ev.WalletID, ev.EventType, ev.CreatedAt, ev.EventData)
	}
}

func handleBenchmarkMenu() {
	fmt.Println("\n[INFO] Benchmark / tuning is available through Go's benchmark runner.")
	fmt.Println("       Run: go test -bench=. ./...")
}

func handleConfigMenu(cfg *config.Config, reader *bufio.Reader) {
	for {
		fmt.Printf(`
  ┌──────────────────────────────────────┐
  │             CONFIGURATION            │
  ├──────────────────────────────────────┤
  │  Database       : %-18s │
  │  User           : %-18s │
  │  Host           : %-18s │
  │  Port           : %-18d │
  │  Max conns      : %-18d │
  │  Min conns      : %-18d │
  │  Batch size     : %-15d wallets │
  │  Log level      : %-18s │
  │  Pool monitor   : %-15d s │
  ├──────────────────────────────────────┤
  │   1   Update batch size              │
  │   2   Back                           │
  └──────────────────────────────────────┘
  Select option: `,
			cfg.DBName,
			cfg.DBUser,
			cfg.DBHost,
			cfg.DBPort,
			cfg.DBMaxConns,
			cfg.DBMinConns,
			cfg.BatchSize,
			cfg.LogLevel,
			cfg.PoolMonitorInterval,
		)

		switch strings.TrimSpace(readLine(reader)) {
		case "1":
			changeGenerationSettings(cfg, reader)
		case "2":
			return
		default:
			fmt.Println("\n[WARN] Invalid option — please choose 1 or 2.")
		}
	}
}

func handleHelpMenu() {
	fmt.Print(`
  Generate wallets     Create wallet batches and store them in PostgreSQL.
  Statistics           Show cached wallet/event counters.
  Wallet lookup        Find one wallet by numeric ID.
  Database tools       Run database health checks.
  Monitoring           Show recent wallet events.
  Benchmark / tuning   Print the benchmark command for this build.
  Configuration        Show loaded runtime settings.
`)
}

func handleGenerate(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, reader *bufio.Reader) {
	for {
		fmt.Print(`
  ┌────────────────────────────────────────────┐
  │              GENERATE WALLETS              │
  │   1   Generate by wallet count             │
  │   2   Generate by batch count              │
  │   3   Change generation settings           │
  │   4   Preview current generation settings  │
  │   5   Back                                 │
  └────────────────────────────────────────────┘
  Select option: `)

		switch strings.TrimSpace(readLine(reader)) {
		case "1":
			total, ok := promptPositiveInt(reader, "\n  Enter number of wallets to generate: ")
			if ok {
				generateWallets(ctx, pool, cfg, total)
			}
		case "2":
			batches, ok := promptPositiveInt(reader, fmt.Sprintf("\n  Enter number of batches (1 batch = %d wallets): ", cfg.BatchSize))
			if !ok {
				continue
			}

			total, ok := generationTotal(batches, cfg.BatchSize)
			if !ok {
				fmt.Printf("\n[ERROR] Total wallets exceeds maximum safe value. Lower batches or batch size (%d).\n", cfg.BatchSize)
				continue
			}

			fmt.Printf("\n[INFO] %d batches × %d wallets = %d wallets\n", batches, cfg.BatchSize, total)
			generateWallets(ctx, pool, cfg, total)
		case "3":
			changeGenerationSettings(cfg, reader)
		case "4":
			previewGenerationSettings(cfg)
		case "5":
			return
		default:
			fmt.Println("\n[WARN] Invalid option — please choose 1 to 5.")
		}
	}
}

func promptPositiveInt(reader *bufio.Reader, prompt string) (int, bool) {
	fmt.Print(prompt)
	n, err := strconv.Atoi(strings.TrimSpace(readLine(reader)))
	if err != nil || n < 1 {
		fmt.Println("\n[ERROR] Please enter a positive integer (e.g. 1, 5, 100).")
		return 0, false
	}
	return n, true
}

func generationTotal(batches, batchSize int) (int, bool) {
	if batches < 1 || batchSize < 1 {
		return 0, false
	}
	total := int64(batches) * int64(batchSize)
	if total > int64(^uint(0)>>1) {
		return 0, false
	}
	return int(total), true
}

func changeGenerationSettings(cfg *config.Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current batch size: %d wallets\n", cfg.BatchSize)
	batchSize, ok := promptPositiveInt(reader, "  Enter new batch size (1-1000 wallets): ")
	if !ok {
		return
	}
	if err := validateBatchSize(batchSize); err != nil {
		fmt.Printf("\n[ERROR] %v\n", err)
		return
	}
	cfg.BatchSize = batchSize
	fmt.Printf("[INFO] Generation batch size set to %d wallets.\n", cfg.BatchSize)
}

func validateBatchSize(batchSize int) error {
	if batchSize < 1 {
		return fmt.Errorf("BATCH_SIZE must be at least 1, got %d", batchSize)
	}
	if batchSize > 1000 {
		return fmt.Errorf("BATCH_SIZE cannot exceed 1000 (PostgreSQL limit), got %d", batchSize)
	}
	return nil
}

func previewGenerationSettings(cfg *config.Config) {
	fmt.Printf(`
  ┌──────────────────────────────────────┐
  │       GENERATION SETTINGS            │
  ├──────────────────────────────────────┤
  │  Batch size : %-21d │
  │  Workers    : %-21d │
  └──────────────────────────────────────┘
`, cfg.BatchSize, cfg.Workers)
}

func generateWallets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, total int) {
	fmt.Printf("\n[INFO] Starting wallet generation\n")
	fmt.Printf("[INFO] Generating %d wallets\n", total)

	if err := core.GenerateWallets(ctx, pool, cfg, total); err != nil {
		fmt.Printf("\n[ERROR] Generation failed: %v\n", err)
		return
	}

	fmt.Printf("[INFO] Generation finished — all %d wallets stored successfully.\n\n", total)
}

func handleStats(ctx context.Context, pool *pgxpool.Pool) {
	fmt.Println("\n[INFO] Loading statistics...")

	s, err := core.GetStats(ctx, pool)
	if err != nil {
		fmt.Printf("[ERROR] Could not load stats: %v\n", err)
		return
	}
	core.PrintStats(s)
}

func handleWalletInfo(ctx context.Context, pool *pgxpool.Pool, reader *bufio.Reader) {
	fmt.Print("\n  Enter wallet ID (numeric): ")
	input := strings.TrimSpace(readLine(reader))

	id, err := strconv.ParseInt(input, 10, 64)
	if err != nil || id < 1 {
		fmt.Println("[ERROR] Please enter a valid wallet ID (positive integer).")
		return
	}

	type walletRow struct {
		ID        int64
		Address   []byte
		CreatedAt time.Time
		Status    int
	}

	var w walletRow
	err = pool.QueryRow(ctx, `
		SELECT id, address, created_at, status
		FROM wallets
		WHERE id = $1
	`, id).Scan(&w.ID, &w.Address, &w.CreatedAt, &w.Status)

	if err == pgx.ErrNoRows {
		fmt.Printf("\n[WARN] No wallet found with ID %d.\n", id)
		return
	}
	if err != nil {
		fmt.Printf("[ERROR] Query failed: %v\n", err)
		return
	}

	var eventCount int64
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_events WHERE wallet_id = $1`, id).Scan(&eventCount)

	fmt.Printf(`
  ╔══════════════════════════════════════════════════════╗
  ║                   WALLET INFO                        ║
  ╠══════════════════════════════════════════════════════╣
  ║  ID          : %-36d ║
  ║  Address     : 0x%-34s ║
  ║  Status      : %-36s ║
  ║  Created at  : %-36s ║
  ║  Events      : %-36d ║
  ╚══════════════════════════════════════════════════════╝
`,
		w.ID,
		hex.EncodeToString(w.Address),
		statusLabel(w.Status),
		w.CreatedAt.Format("2006-01-02 15:04:05 UTC"),
		eventCount,
	)
}

// handleRecentEvents removed - we now use batch-level logging instead of per-wallet events

func handleDatabaseHealth(ctx context.Context, pool *pgxpool.Pool) {
	if err := core.RunHealthCheck(ctx, pool); err != nil {
		fmt.Printf("[ERROR] Health check failed: %v\n", err)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func printBanner() {
	fmt.Print(`
  ╔═══════════════════════════════════════════════════════╗
  ║                                                       ║
  ║         EVM  WALLET  MANAGER   v1.0                   ║
  ║         Multi-chain  ·  PostgreSQL  ·  Go             ║
  ║                                                       ║
  ║   Chains: ETH · BSC · Polygon · Arbitrum · Base       ║
  ║                                                       ║
  ╚═══════════════════════════════════════════════════════╝
`)
}

func printMenu() {
	fmt.Print(`
  ┌──────────────────────────────────────┐
  │   1   Generate wallets               │
  │   2   Statistics                     │
  │   3   Wallet lookup                  │
  │   4   Database tools                 │
  │   5   Monitoring                     │
  │   6   Benchmark / tuning             │
  │   7   Configuration                  │
  │   8   Help                           │
  │   9   Exit                           │
  └──────────────────────────────────────┘
  Select option: `)
}

func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

func statusLabel(s int) string {
	switch s {
	case 0:
		return "unused"
	case 1:
		return "used"
	case 2:
		return "reserved"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}
