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

const walletBatchSize = 1000 // 1 user-facing batch = 1000 wallets

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
			handleGenerate(ctx, pool, cfg, reader)
		case "2":
			handleStats(ctx, pool)
		case "3":
			handleWalletInfo(ctx, pool, reader)
		case "4":
			handleDatabaseHealth(ctx, pool)
		case "5":
			fmt.Println("\n[INFO] Goodbye.\n")
			return
		default:
			fmt.Println("\n[WARN] Invalid option — please choose 1 to 5.")
		}
	}
}

// ─── Menu handlers ────────────────────────────────────────────────────────────

func handleGenerate(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, reader *bufio.Reader) {
	fmt.Print("\n  Enter number of wallet batches (1 batch = 1000 wallets): ")
	input := strings.TrimSpace(readLine(reader))

	batches, err := strconv.Atoi(input)
	if err != nil || batches < 1 {
		fmt.Println("\n[ERROR] Please enter a positive integer (e.g. 1, 5, 100).")
		return
	}

	const maxBatches = 10_000
	if batches > maxBatches {
		fmt.Printf("\n[ERROR] Maximum is %d batches (%d wallets) per run.\n",
			maxBatches, maxBatches*walletBatchSize)
		fmt.Println("        Run the generator multiple times for larger totals.")
		return
	}

	// ponytail: Prevent integer overflow on large wallet counts
	totalInt64 := int64(batches) * int64(walletBatchSize)
	if totalInt64 > 2147483647 { // Max int32
		fmt.Printf("\n[ERROR] Total wallets (%d) exceeds maximum safe value.\n", totalInt64)
		return
	}
	total := int(totalInt64)

	fmt.Printf("\n[INFO] Starting wallet generation\n")
	fmt.Printf("[INFO] Generating %d wallets (%d batch(es) of %d)\n",
		total, batches, walletBatchSize)

	if err := core.GenerateWallets(ctx, pool, cfg, total); err != nil {
		fmt.Printf("\n[ERROR] Generation failed: %v\n", err)
		return
	}

	fmt.Printf("[INFO] Batch finished — all %d wallets stored successfully.\n\n", total)
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
  │   2   Show statistics                │
  │   3   Show wallet info               │
  │   4   Database health                │
  │   5   Exit                           │
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