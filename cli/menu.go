// Package cli implements the interactive command-line menu.
package cli

import (
	"bufio"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"evmwalletbot/config"
	"evmwalletbot/core"
	"evmwalletbot/events"
)

const walletBatchSize = 1000 // 1 user-facing batch = 1000 wallets

// Run is the main entry point for the interactive CLI.
func Run(db *sql.DB, cfg *config.Config) {
	reader := bufio.NewReader(os.Stdin)
	printBanner()

	for {
		printMenu()
		choice := strings.TrimSpace(readLine(reader))

		switch choice {
		case "1":
			handleGenerate(db, cfg, reader)
		case "2":
			handleStats(db)
		case "3":
			handleWalletInfo(db, reader)
		case "4":
			handleRecentEvents(db)
		case "5":
			fmt.Println("\n[INFO] Goodbye.\n")
			return
		default:
			fmt.Println("\n[WARN] Invalid option — please choose 1 to 5.")
		}
	}
}

// ─── Menu handlers ────────────────────────────────────────────────────────────

func handleGenerate(db *sql.DB, cfg *config.Config, reader *bufio.Reader) {
	fmt.Print("\n  Enter number of wallet batches (1 batch = 1000 wallets): ")
	input := strings.TrimSpace(readLine(reader))

	batches, err := strconv.Atoi(input)
	if err != nil || batches < 1 {
		fmt.Println("\n[ERROR] Please enter a positive integer (e.g. 1, 5, 100).")
		return
	}

	// BUG FIX #7 — safety cap: max 10,000 batches (10M wallets) per run.
	// Without this, a typo like "100000" silently queues 100M wallets.
	const maxBatches = 10_000
	if batches > maxBatches {
		fmt.Printf("\n[ERROR] Maximum is %d batches (%d wallets) per run.\n",
			maxBatches, maxBatches*walletBatchSize)
		fmt.Println("        Run the generator multiple times for larger totals.")
		return
	}

	total := batches * walletBatchSize

	// DB is already connected since startup — no reconnect happens here.
	fmt.Printf("\n[INFO] Starting wallet generation\n")
	fmt.Printf("[INFO] Generating %d wallets (%d batch(es) of %d)\n",
		total, batches, walletBatchSize)

	if err := core.GenerateWallets(db, cfg, total); err != nil {
		fmt.Printf("\n[ERROR] Generation failed: %v\n", err)
		return
	}

	fmt.Printf("[INFO] Batch finished — all %d wallets stored successfully.\n\n", total)
}

func handleStats(db *sql.DB) {
	fmt.Println("\n[INFO] Loading statistics...")

	s, err := core.GetStats(db)
	if err != nil {
		fmt.Printf("[ERROR] Could not load stats: %v\n", err)
		return
	}
	core.PrintStats(s)
}

func handleWalletInfo(db *sql.DB, reader *bufio.Reader) {
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
	err = db.QueryRow(`
		SELECT id, address, created_at, status
		FROM wallets
		WHERE id = $1
	`, id).Scan(&w.ID, &w.Address, &w.CreatedAt, &w.Status)

	if err == sql.ErrNoRows {
		fmt.Printf("\n[WARN] No wallet found with ID %d.\n", id)
		return
	}
	if err != nil {
		fmt.Printf("[ERROR] Query failed: %v\n", err)
		return
	}

	// Count events for this wallet
	var eventCount int64
	_ = db.QueryRow(`SELECT COUNT(*) FROM wallet_events WHERE wallet_id = $1`, id).Scan(&eventCount)

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

func handleRecentEvents(db *sql.DB) {
	fmt.Println("\n[INFO] Fetching recent events...")

	evList, err := events.GetRecent(db, 20)
	if err != nil {
		fmt.Printf("[ERROR] Could not fetch events: %v\n", err)
		return
	}

	if len(evList) == 0 {
		fmt.Println("[INFO] No events recorded yet.")
		return
	}

	fmt.Printf("\n  %-6s  %-10s  %-22s  %-20s  %s\n",
		"ID", "WALLET_ID", "TYPE", "CREATED_AT", "DATA")
	fmt.Println("  " + strings.Repeat("─", 88))

	for _, ev := range evList {
		data := ev.EventData
		if len(data) > 36 {
			data = data[:33] + "..."
		}
		fmt.Printf("  %-6d  %-10d  %-22s  %-20s  %s\n",
			ev.ID, ev.WalletID, ev.EventType, ev.CreatedAt, data)
	}
	fmt.Println()
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
  │   4   Show recent events             │
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
