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
	"evmwalletbot/wallet"
)

// Run is the main entry point for the interactive CLI.
func Run(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
	reader := bufio.NewReader(os.Stdin)

	// Clear screen and show banner on startup
	core.ClearScreenIfEnabled()
	printBanner()

	// Show first-run tips if enabled
	if cfg.ShowFirstRunTips {
		showFirstRunTips(reader, cfg)
	}

	for {
		// Check if context is cancelled (graceful shutdown)
		select {
		case <-ctx.Done():
			fmt.Println(core.Info("\n[INFO] Shutdown requested, exiting..."))
			return
		default:
		}

		// Show status strip
		if strip, err := GetStatusStrip(ctx, pool, cfg); err == nil {
			strip.Render()
		}

		printMenu(cfg)
		choice := normalizeMenuChoice(readLine(reader))

		// Clear screen before handling choice
		core.ClearScreenIfEnabled()

		switch choice {
		case "1":
			handleGenerateMenu(ctx, pool, cfg, reader)
		case "2":
			handleStatsMenu(ctx, pool, reader)
		case "3":
			handleLookupMenu(ctx, pool, reader)
		case "4":
			handleDatabaseMenu(ctx, pool, reader)
		case "5":
			handleBenchmarkMenu(ctx, pool, cfg, reader)
		case "6":
			handleConfigMenu(cfg, reader)
		case "8":
			handleHelpMenu(reader)
		case "9":
			handleVanityMenu(ctx, pool, cfg, reader)
		case "0":
			fmt.Println(core.Info("\n[INFO] Goodbye."))
			return
		default:
			fmt.Println(core.Warning("\n[WARN] Invalid option — please choose 1-9 or 0."))
		}
	}
}

// ─── Menu handlers ────────────────────────────────────────────────────────────

func handleGenerateMenu(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, reader *bufio.Reader) {
	// Flattened: Direct inline prompts instead of submenu
	core.ClearScreenIfEnabled()

	fmt.Printf("\n%s\n", core.Highlight("GENERATE WALLETS"))
	fmt.Printf("  Current settings: %d workers, batch size %d\n\n", cfg.Workers, cfg.BatchSize)

	fmt.Print("  Enter wallet count (or 'b' for batch mode, 's' for settings): ")
	input := strings.ToLower(strings.TrimSpace(readLine(reader)))

	if input == "" {
		return
	}

	if input == "s" || input == "settings" {
		changeGenerationSettings(cfg, reader)
		handleGenerateMenu(ctx, pool, cfg, reader) // Recursive call to show menu again
		return
	}

	if input == "b" || input == "batch" {
		// Batch mode
		fmt.Printf("\n  Enter batch count (1 batch = %d wallets): ", cfg.BatchSize)
		batchInput := strings.TrimSpace(readLine(reader))

		batches, err := strconv.Atoi(batchInput)
		if err != nil || batches < 1 {
			fmt.Println(core.Error("\n[ERROR] Please enter a valid positive number for batches."))
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
			return
		}

		total, ok := generationTotal(batches, cfg.BatchSize)
		if !ok {
			fmt.Printf(core.Error("\n[ERROR] Total wallets exceeds maximum safe value.\n"))
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
			return
		}

		fmt.Printf(core.Info("\n[INFO] %d batches × %d wallets = %s wallets\n", batches, cfg.BatchSize, formatNumber(total)))
		generateWallets(ctx, pool, cfg, total)
		return
	}

	// Direct count mode
	count, err := strconv.Atoi(input)
	if err != nil || count < 1 {
		fmt.Println(core.Error("\n[ERROR] Please enter a valid positive number."))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	generateWallets(ctx, pool, cfg, count)
}

func handleStatsMenu(ctx context.Context, pool *pgxpool.Pool, reader *bufio.Reader) {
	// Flattened: Show stats immediately with action options
	core.ClearScreenIfEnabled()

	// Show stats immediately
	handleStats(ctx, pool)

	// Offer quick actions
	fmt.Printf("\n  %s watch live   %s database size   %s refresh   %s back\n",
		core.Hint("[W]"), core.Hint("[D]"), core.Hint("[R]"), core.Hint("[Enter]"))
	fmt.Print("  Action: ")

	choice := strings.ToLower(strings.TrimSpace(readLine(reader)))

	switch choice {
	case "w", "watch":
		watchStatsLive(ctx, pool)
	case "d", "size":
		showDatabaseSize(ctx, pool)
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
	case "r", "refresh":
		handleStatsMenu(ctx, pool, reader) // Recursive refresh
	case "":
		return // Back to main menu
	default:
		return
	}
}

func handleLookupMenu(ctx context.Context, pool *pgxpool.Pool, reader *bufio.Reader) {
	handleWalletInfo(ctx, pool, reader)
}

func handleDatabaseMenu(ctx context.Context, pool *pgxpool.Pool, reader *bufio.Reader) {
	// Combined Database + Monitoring menu (Tier 2 flattening)
	for {
		core.ClearScreenIfEnabled()

		fmt.Print(`
  ┌────────────────────────────────────────────┐
  │         DATABASE & MONITORING              │
  │   1   Health check                         │
  │   2   Connection pool status               │
  │   3   Watch pool live                      │
  │   4   Watch wallet stats live              │
  │   5   Record health snapshot               │
  │   6   Maintenance recommendations          │
  │   7   Database size                        │
  │   8   Back                                 │
  └────────────────────────────────────────────┘
  Select option: `)

		switch strings.TrimSpace(readLine(reader)) {
		case "1":
			handleDatabaseHealth(ctx, pool)
		case "2":
			showPoolStatus(pool)
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
		case "3":
			watchPoolStatusLive(ctx, pool, 5)
		case "4":
			watchWalletStatsLive(ctx, pool, 5)
		case "5":
			recordHealthSnapshot(ctx, pool)
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
		case "6":
			showMaintenanceRecommendations(ctx, pool)
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
		case "7":
			showDatabaseSize(ctx, pool)
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
		case "8":
			return
		default:
			fmt.Println(core.Warning("\n[WARN] Invalid option — please choose 1 to 8."))
		}
	}
}

func handleVanityMenu(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, reader *bufio.Reader) {
	core.ClearScreenIfEnabled()

	fmt.Printf("\n%s\n", core.Highlight("VANITY ADDRESS GENERATION"))
	fmt.Println("  Generate wallets with custom prefix and/or suffix patterns")
	fmt.Println()

	// Prompt for prefix (optional)
	var prefix string
	for {
		fmt.Print("  Enter prefix pattern (optional, press Enter to skip): ")
		prefix = strings.TrimSpace(readLine(reader))

		if prefix == "" {
			break
		}

		if err := wallet.ValidateVanityPattern(prefix, "prefix"); err != nil {
			fmt.Printf(core.Error("\n[ERROR] %v\n"), err)
			fmt.Println("        Valid characters: 0-9, a-f, A-F")
			fmt.Println()
			continue
		}
		break
	}

	// Prompt for suffix (optional)
	var suffix string
	for {
		fmt.Print("  Enter suffix pattern (optional, press Enter to skip): ")
		suffix = strings.TrimSpace(readLine(reader))

		if suffix == "" {
			break
		}

		if err := wallet.ValidateVanityPattern(suffix, "suffix"); err != nil {
			fmt.Printf(core.Error("\n[ERROR] %v\n"), err)
			fmt.Println("        Valid characters: 0-9, a-f, A-F")
			fmt.Println()
			continue
		}
		break
	}

	// Check if at least one pattern is provided
	if prefix == "" && suffix == "" {
		fmt.Println(core.Warning("\n[WARN] No pattern specified. Use 'Generate wallets' menu for random generation."))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	// Prompt for checksum mode
	fmt.Print("  Case-sensitive matching (harder)? [y/N]: ")
	checksumInput := strings.ToLower(strings.TrimSpace(readLine(reader)))
	checksum := checksumInput == "y" || checksumInput == "yes"

	// Prompt for wallet count
	var count int
	for {
		countVal, ok := promptPositiveInt(reader, "  Number of matching wallets to generate: ")
		if !ok {
			continue
		}
		count = countVal
		break
	}

	// Create vanity config
	vanityConfig := core.VanityConfig{
		Prefix:      prefix,
		Suffix:      suffix,
		Checksum:    checksum,
		TargetCount: count,
	}

	// Generate vanity wallets
	if err := core.GenerateVanityWallets(ctx, pool, cfg, vanityConfig); err != nil {
		fmt.Printf(core.Error("\n[ERROR] Vanity generation failed: %v\n"), err)
	}

	fmt.Print("\n  Press Enter to continue...")
	readLine(reader)
}

func handleBenchmarkMenu(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, reader *bufio.Reader) {
	for {
		fmt.Print(`
  ┌────────────────────────────────────────────┐
  │           BENCHMARK / TUNING               │
  │   1   Estimate current settings            │
  │   2   Run small benchmark (1000 wallets)   │
  │   3   Compare worker counts                │
  │   4   Compare batch sizes                  │
  │   5   Back                                 │
  └────────────────────────────────────────────┘
  Select option: `)

		switch strings.TrimSpace(readLine(reader)) {
		case "1":
			estimateSettings(cfg)
		case "2":
			runSmallBenchmark(ctx, pool, cfg)
		case "3":
			compareWorkerCounts(ctx, pool, cfg)
		case "4":
			compareBatchSizes(ctx, pool, cfg)
		case "5":
			return
		default:
			fmt.Println(core.Warning("\n[WARN] Invalid option — please choose 1 to 5."))
		}
	}
}

func handleConfigMenu(cfg *config.Config, reader *bufio.Reader) {
	// Store original config for reset functionality
	originalCfg := *cfg

	for {
		loggingStatus := "enabled"
		if !cfg.EnableLogging {
			loggingStatus = "disabled"
		}

		tipsStatus := "enabled"
		if !cfg.ShowFirstRunTips {
			tipsStatus = "disabled"
		}

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
  │  Workers        : %-18d │
  │  Batch size     : %-15d wallets │
  │  Logging        : %-18s │
  │  Pool monitor   : %-15d s │
  │  Pool threshold : %-18.2f │
  │  UI mode        : %-18s │
  │  First-run tips : %-18s │
  ├──────────────────────────────────────┤
  │   1   Show current settings          │
  │   2   Workers                        │
  │   3   Batch size                     │
  │   4   Logging (enable/disable)       │
  │   5   Pool monitor interval          │
  │   6   Pool warning threshold         │
  │   7   UI mode (full/minimal)         │
  │   8   First-run tips (toggle)        │
  │   9   Reset session settings         │
  │   0   Back                           │
  └──────────────────────────────────────┘
  Select option: `,
			cfg.DBName,
			cfg.DBUser,
			cfg.DBHost,
			cfg.DBPort,
			cfg.DBMaxConns,
			cfg.DBMinConns,
			cfg.Workers,
			cfg.BatchSize,
			loggingStatus,
			cfg.PoolMonitorInterval,
			cfg.PoolWarningThreshold,
			cfg.UIMode,
			tipsStatus,
		)

		switch strings.TrimSpace(readLine(reader)) {
		case "1":
			showCurrentSettings(cfg)
		case "2":
			changeWorkers(cfg, reader)
		case "3":
			changeGenerationSettings(cfg, reader)
		case "4":
			toggleLogging(cfg)
		case "5":
			changePoolMonitorInterval(cfg, reader)
		case "6":
			changePoolWarningThreshold(cfg, reader)
		case "7":
			toggleUIMode(cfg)
		case "8":
			toggleFirstRunTips(cfg)
		case "9":
			resetSessionSettings(cfg, &originalCfg)
		case "0":
			return
		default:
			fmt.Println(core.Warning("\n[WARN] Invalid option — please choose 1-9 or 0."))
		}
	}
}

func handleHelpMenu(reader *bufio.Reader) {
	for {
		fmt.Print(`
  ┌────────────────────────────────────────────┐
  │                  HELP                      │
  │   1   Generation modes                     │
  │   2   Batch size guide                     │
  │   3   Workers guide                        │
  │   4   Database guide                       │
  │   5   Settings guide                       │
  │   6   Back                                 │
  └────────────────────────────────────────────┘
  Select option: `)

		switch strings.TrimSpace(readLine(reader)) {
		case "1":
			showGenerationHelp()
		case "2":
			showBatchSizeHelp()
		case "3":
			showWorkersHelp()
		case "4":
			showDatabaseHelp()
		case "5":
			showSettingsHelp()
		case "6":
			return
		default:
			fmt.Println(core.Warning("\n[WARN] Invalid option — please choose 1 to 6."))
		}
	}
}

func promptPositiveInt(reader *bufio.Reader, prompt string) (int, bool) {
	fmt.Print(prompt)
	input := strings.TrimSpace(readLine(reader))

	if input == "" {
		fmt.Println(core.Error("\n[ERROR] Input cannot be empty."))
		fmt.Println("        Please enter a positive integer (e.g., 1, 5, 100).") 
		return 0, false
	}

	n, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf(core.Error("\n[ERROR] Invalid input: '%s' is not a valid number.\n", input))
		fmt.Println("        Please enter a positive integer (e.g., 1, 5, 100).")
		fmt.Println("        Examples: 1000, 50000, 1000000")
		return 0, false
	}

	if n < 1 {
		fmt.Printf(core.Error("\n[ERROR] Invalid value: %d is not positive.\n", n))
		fmt.Println("        Please enter a number greater than 0.")
		fmt.Println("        Minimum value: 1")
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
		fmt.Printf(core.Error("\n[ERROR] %v\n", err))
		return
	}
	cfg.BatchSize = batchSize
	fmt.Printf(core.Info("[INFO] Generation batch size set to %d wallets.\n", cfg.BatchSize))
}

func validateBatchSize(batchSize int) error {
	if batchSize < 1 {
		return fmt.Errorf("batch size must be at least 1, got %d\n        Minimum value: 1\n        Recommended: 500-1000", batchSize)
	}
	if batchSize > 1000 {
		return fmt.Errorf("batch size cannot exceed 1000 (PostgreSQL COPY limit), got %d\n        Maximum value: 1000\n        Recommended: 500-1000 for best performance", batchSize)
	}
	return nil
}

func generateWallets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, total int) {
	// Show preview for the run
	previewGenerationRun(cfg, total)

	// Ask for confirmation if total exceeds threshold (10,000 wallets)
	if total > 10000 {
		if !confirmGeneration(total) {
			fmt.Println(core.Info("\n[INFO] Generation cancelled by user."))
			return
		}
	}

	fmt.Printf(core.Info("\n[INFO] Starting wallet generation\n"))
	fmt.Printf(core.Info("[INFO] Generating %d wallets\n", total))

	start := time.Now()

	if err := core.GenerateWallets(ctx, pool, cfg, total); err != nil {
		fmt.Printf(core.Error("\n[ERROR] Generation failed: %v\n", err))
		return
	}

	elapsed := time.Since(start)

	// Show completion summary
	showCompletionSummary(total, elapsed, cfg)
}

func handleStats(ctx context.Context, pool *pgxpool.Pool) {
	fmt.Println(core.Info("\n[INFO] Loading statistics..."))

	s, err := core.GetStats(ctx, pool)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not load stats: %v\n", err))
		return
	}
	core.PrintStats(s)
}

func handleWalletInfo(ctx context.Context, pool *pgxpool.Pool, reader *bufio.Reader) {
	fmt.Print("\n  Enter wallet ID (numeric): ")
	input := strings.TrimSpace(readLine(reader))

	id, err := strconv.ParseInt(input, 10, 64)
	if err != nil || id < 1 {
		fmt.Println(core.Error("[ERROR] Please enter a valid wallet ID (positive integer)."))
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
		fmt.Printf(core.Warning("\n[WARN] No wallet found with ID %d.\n", id))
		return
	}
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Query failed: %v\n", err))
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

func handleDatabaseHealth(ctx context.Context, pool *pgxpool.Pool) {
	if err := core.RunHealthCheck(ctx, pool); err != nil {
		fmt.Printf(core.Error("[ERROR] Health check failed: %v\n", err))
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func printBanner() {
	title := core.Highlight("EVM WALLET MANAGER") + " v1.0"
	subtitle := core.Hint("Multi-chain · PostgreSQL · Go")
	chains := core.Info("ETH · BSC · Polygon · Arbitrum · Optimism · Base")

	fmt.Printf(`
  ╔═══════════════════════════════════════════════════════╗
  ║                                                       ║
  ║         %s                   ║
  ║         %s             ║
  ║                                                       ║
  ║   %s       ║
  ║                                                       ║
  ╚═══════════════════════════════════════════════════════╝
`, title, subtitle, chains)
}

// normalizeMenuChoice converts letter shortcuts to number choices.
func normalizeMenuChoice(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))

	// Letter shortcuts for main menu (updated for 9-item menu)
	shortcuts := map[string]string{
		"g": "1", // Generate
		"s": "2", // Statistics
		"l": "3", // Lookup
		"d": "4", // Database & Monitoring (combined)
		"b": "5", // Benchmark
		"c": "6", // Configuration
		"h": "8", // Help
		"v": "9", // Vanity
		"q": "0", // Quit
		"x": "0", // Alternative quit
	}

	if mapped, ok := shortcuts[input]; ok {
		return mapped
	}
	return input
}

func printMenu(cfg *config.Config) {
	title := core.Highlight("MAIN MENU")

	// Generate hint showing current settings
	genHint := core.Hint(fmt.Sprintf("batch %d", cfg.BatchSize))
	settingsHint := core.Hint(fmt.Sprintf("%d workers", cfg.Workers))

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │   %s                                        │
  ├──────────────────────────────────────────────────────┤
  │   %s %s   Generate wallets               %s │
  │   %s %s   Statistics                                 │
  │   %s %s   Wallet lookup                              │
  │   %s %s   Database & monitoring                      │
  │   %s %s   Benchmark / tuning                         │
  │   %s %s   Configuration                  %s │
  │   %s %s   Help                                       │
  │   %s %s   Vanity address                             │
  │   %s %s   Exit                                       │
  └──────────────────────────────────────────────────────┘
  %s `,
		title,
		core.Success("1"), core.Hint("[G]"), genHint,
		core.Info("2"), core.Hint("[S]"),
		core.Info("3"), core.Hint("[L]"),
		core.Info("4"), core.Hint("[D]"),
		core.Info("5"), core.Hint("[B]"),
		core.Info("6"), core.Hint("[C]"), settingsHint,
		core.Info("8"), core.Hint("[H]"),
		core.Success("9"), core.Hint("[V]"),
		core.Warning("0"), core.Hint("[Q]"),
		core.Hint("Select option:"))
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

// ─── Configuration Menu Helpers ───────────────────────────────────────────────

func showCurrentSettings(cfg *config.Config) {
	loggingStatus := "enabled"
	if !cfg.EnableLogging {
		loggingStatus = "disabled"
	}

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │              CURRENT SETTINGS                        │
  ├──────────────────────────────────────────────────────┤
  │  Database Configuration:                             │
  │    Host           : %-33s │
  │    Port           : %-33d │
  │    Database       : %-33s │
  │    User           : %-33s │
  │    Max conns      : %-33d │
  │    Min conns      : %-33d │
  │                                                       │
  │  Generation Settings:                                │
  │    Workers        : %-33d │
  │    Batch size     : %-30d wallets │
  │                                                       │
  │  Monitoring Settings:                                │
  │    Logging        : %-33s │
  │    Pool monitor   : %-30d seconds │
  │    Pool threshold : %-33.2f │
  │                                                       │
  │  Note: Changes apply to current session only.        │
  │        Use option 9 to reset to .env defaults.       │
  └──────────────────────────────────────────────────────┘
`,
		cfg.DBHost,
		cfg.DBPort,
		cfg.DBName,
		cfg.DBUser,
		cfg.DBMaxConns,
		cfg.DBMinConns,
		cfg.Workers,
		cfg.BatchSize,
		loggingStatus,
		cfg.PoolMonitorInterval,
		cfg.PoolWarningThreshold,
	)
}

func changeWorkers(cfg *config.Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current workers: %d\n", cfg.Workers)
	fmt.Println("  Recommended: 8-32 (based on CPU cores)")
	fmt.Println("  Higher values increase throughput but use more CPU/memory")

	workers, ok := promptPositiveInt(reader, "  Enter new worker count (1-100): ")
	if !ok {
		return
	}

	if workers < 1 || workers > 100 {
		fmt.Println(core.Error("\n[ERROR] Worker count must be between 1 and 100."))
		fmt.Printf("        Current value: %d\n", cfg.Workers)
		fmt.Println("        Recommended: 8-32 for most systems")
		return
	}

	cfg.Workers = workers
	fmt.Printf(core.Info("[INFO] Workers set to %d for this session.\n", cfg.Workers))
}

func toggleLogging(cfg *config.Config) {
	cfg.EnableLogging = !cfg.EnableLogging

	status := "enabled"
	if !cfg.EnableLogging {
		status = "disabled"
	}
	fmt.Printf(core.Info("\n[INFO] Logging %s for this session.\n", status))
	if !cfg.EnableLogging {
		fmt.Println("       Note: Error and warning messages will still be shown.")
	}
}

func changePoolMonitorInterval(cfg *config.Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current pool monitor interval: %d seconds\n", cfg.PoolMonitorInterval)
	fmt.Println("  Set to 0 to disable pool monitoring")
	fmt.Println("  Recommended: 30-60 seconds for production")

	interval, ok := promptPositiveInt(reader, "  Enter new interval in seconds (0-300): ")
	if !ok {
		return
	}

	if interval < 0 || interval > 300 {
		fmt.Println(core.Error("\n[ERROR] Pool monitor interval must be between 0 and 300 seconds."))
		fmt.Printf("        Current value: %d seconds\n", cfg.PoolMonitorInterval)
		fmt.Println("        Set to 0 to disable, or 30-60 for normal monitoring")
		return
	}

	cfg.PoolMonitorInterval = interval
	if interval == 0 {
		fmt.Println(core.Info("[INFO] Pool monitoring disabled for this session."))
	} else {
		fmt.Printf(core.Info("[INFO] Pool monitor interval set to %d seconds for this session.\n", cfg.PoolMonitorInterval))
	}
}

func changePoolWarningThreshold(cfg *config.Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current pool warning threshold: %.2f\n", cfg.PoolWarningThreshold)
	fmt.Println("  This is the ratio of used/total connections that triggers a warning")
	fmt.Println("  Recommended: 0.7-0.9 (70%-90%)")

	fmt.Print("  Enter new threshold (0.1-1.0): ")
	input := strings.TrimSpace(readLine(reader))

	threshold, err := strconv.ParseFloat(input, 64)
	if err != nil {
		fmt.Println(core.Error("\n[ERROR] Please enter a valid decimal number (e.g., 0.8)"))
		return
	}

	if threshold <= 0 || threshold > 1.0 {
		fmt.Println(core.Error("\n[ERROR] Pool warning threshold must be between 0.1 and 1.0."))
		fmt.Printf("        Current value: %.2f\n", cfg.PoolWarningThreshold)
		fmt.Println("        Recommended: 0.7-0.9 (70%-90%)")
		return
	}

	cfg.PoolWarningThreshold = threshold
	fmt.Printf(core.Info("[INFO] Pool warning threshold set to %.2f for this session.\n", cfg.PoolWarningThreshold))
}

func toggleUIMode(cfg *config.Config) {
	if cfg.UIMode == "full" {
		cfg.UIMode = "minimal"
		fmt.Printf(core.Info("\n[INFO] UI mode set to 'minimal' for this session.\n"))
		fmt.Println("       Minimal mode shows less decorative elements and focuses on essential information.")
	} else {
		cfg.UIMode = "full"
		fmt.Printf(core.Info("\n[INFO] UI mode set to 'full' for this session.\n"))
		fmt.Println("       Full mode shows all decorative elements and detailed information.")
	}
}

func toggleFirstRunTips(cfg *config.Config) {
	cfg.ShowFirstRunTips = !cfg.ShowFirstRunTips

	status := "enabled"
	if !cfg.ShowFirstRunTips {
		status = "disabled"
	}
	fmt.Printf(core.Info("\n[INFO] First-run tips %s for this session.\n", status))
	if cfg.ShowFirstRunTips {
		fmt.Println("       Tips will be shown on next application start.")
	} else {
		fmt.Println("       Tips will not be shown on application start.")
	}
}

func resetSessionSettings(cfg *config.Config, originalCfg *config.Config) {
	fmt.Print("\n  Reset all settings to .env defaults? [y/N]: ")
	input := strings.ToLower(strings.TrimSpace(readLine(bufio.NewReader(os.Stdin))))

	if input != "y" && input != "yes" {
		fmt.Println(core.Info("[INFO] Reset cancelled."))
		return
	}

	// Restore original configuration
	cfg.Workers = originalCfg.Workers
	cfg.BatchSize = originalCfg.BatchSize
	cfg.EnableLogging = originalCfg.EnableLogging
	cfg.PoolMonitorInterval = originalCfg.PoolMonitorInterval
	cfg.PoolWarningThreshold = originalCfg.PoolWarningThreshold
	fmt.Println(core.Info("[INFO] All session settings reset to .env defaults."))
}

// ─── Generation Preview & Confirmation ────────────────────────────────────────

func previewGenerationRun(cfg *config.Config, total int) {
	batches := (total + cfg.BatchSize - 1) / cfg.BatchSize // Ceiling division
	loggingStatus := "enabled"
	if !cfg.EnableLogging {
		loggingStatus = "disabled"
	}

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │                  RUN PREVIEW                         │
  ├──────────────────────────────────────────────────────┤
  │  Wallets        : %-33d │
  │  Mode           : %d batches × %d wallets%-11s │
  │  Workers        : %-33d │
  │  Batch size     : %-33d │
  │  Insert batches : %-33d │
  │  Database       : %-33s │
  │  Logging        : %-33s │
  └──────────────────────────────────────────────────────┘
`,
		total,
		batches, cfg.BatchSize, "",
		cfg.Workers,
		cfg.BatchSize,
		batches,
		cfg.DBName,
		loggingStatus,
	)
}

func confirmGeneration(total int) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\n  ⚠️  Large generation run: %d wallets\n", total)
	fmt.Print("  Continue? [y/N]: ")

	input := strings.ToLower(strings.TrimSpace(readLine(reader)))
	return input == "y" || input == "yes"
}

// ─── Completion Summary ───────────────────────────────────────────────────────

func showCompletionSummary(total int, elapsed time.Duration, cfg *config.Config) {
	walletsPerSec := float64(total) / elapsed.Seconds()

	// Skip detailed summary in minimal UI mode
	if isMinimalUI(cfg) {
		fmt.Printf(core.Info("[INFO] ✓ Generated %s wallets in %s (~%.0f wallets/sec)\n\n",
			formatNumber(total), elapsed.Round(time.Millisecond), walletsPerSec))
		return
	}

	// Full UI mode - show detailed summary
	fmt.Printf(`
  ╔══════════════════════════════════════════════════════════════╗
  ║                    GENERATION COMPLETE                       ║
  ╠══════════════════════════════════════════════════════════════╣
  ║                                                              ║
  ║  ✓ Successfully generated and stored all wallets             ║
  ║                                                              ║
  ║  Summary:                                                    ║
  ║    Wallets generated : %-37s ║
  ║    Time elapsed      : %-37s ║
  ║    Throughput        : ~%-34.0f wallets/sec ║
  ║    Workers used      : %-37d ║
  ║    Batch size        : %-37d ║
  ║                                                              ║
  ║  Performance:                                                ║
  ║    Average per batch : %-37s ║
  ║    Database inserts  : %-37d batches ║
  ║                                                              ║
  ╚══════════════════════════════════════════════════════════════╝

`,
		formatNumber(total),
		elapsed.Round(time.Millisecond),
		walletsPerSec,
		cfg.Workers,
		cfg.BatchSize,
		time.Duration(elapsed.Nanoseconds()/int64((total+cfg.BatchSize-1)/cfg.BatchSize)).Round(time.Millisecond),
		(total+cfg.BatchSize-1)/cfg.BatchSize,
	)
}

// ─── Statistics Menu Helpers ──────────────────────────────────────────────────

func watchStatsLive(ctx context.Context, pool *pgxpool.Pool) {
	fmt.Println(core.Info("\n[INFO] Starting live stats watch (press Ctrl+C to stop)..."))
	fmt.Println("       Refreshing every 5 seconds\n")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Show initial stats
	s, err := core.GetStats(ctx, pool)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not load stats: %v\n", err))
		return
	}
	core.PrintStats(s)

	// Create a channel to detect user interrupt
	done := make(chan bool)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		reader.ReadString('\n')
		done <- true
	}()

	for {
		select {
		case <-ctx.Done():
			fmt.Println(core.Info("\n[INFO] Watch stopped (context cancelled)"))
			return
		case <-done:
			fmt.Println(core.Info("\n[INFO] Watch stopped by user"))
			return
		case <-ticker.C:
			// Clear screen (ANSI escape code)
			fmt.Print("\033[H\033[2J")

			s, err := core.GetStats(ctx, pool)
			if err != nil {
				fmt.Printf(core.Error("[ERROR] Could not load stats: %v\n", err))
				return
			}
			core.PrintStats(s)
			fmt.Println(core.Info("\n[INFO] Press Enter to stop watching..."))
		}
	}
}

func showDatabaseSize(ctx context.Context, pool *pgxpool.Pool) {
	fmt.Println(core.Info("\n[INFO] Loading database size information..."))

	var dbSize string
	err := pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_database_size($1))`, "walletdb").Scan(&dbSize)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not get database size: %v\n", err))
		return
	}

	var walletTableSize, walletIndexSize string
	err = pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size('wallets'))`).Scan(&walletTableSize)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not get wallets table size: %v\n", err))
		return
	}

	err = pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_indexes_size('wallets'))`).Scan(&walletIndexSize)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not get wallets index size: %v\n", err))
		return
	}

	var eventsTableSize string
	err = pool.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size('wallet_events'))`).Scan(&eventsTableSize)
	if err != nil {
		// Table might not exist, that's okay
		eventsTableSize = "N/A"
	}

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │                DATABASE SIZE                         │
  ├──────────────────────────────────────────────────────┤
  │  Total database   : %-33s │
  │  Wallets table    : %-33s │
  │  Wallets indexes  : %-33s │
  │  Events table     : %-33s │
  └──────────────────────────────────────────────────────┘
`,
		dbSize,
		walletTableSize,
		walletIndexSize,
		eventsTableSize,
	)
}

// ─── Database Tools Menu Helpers ──────────────────────────────────────────────

func showPoolStatus(pool *pgxpool.Pool) {
	stat := pool.Stat()

	totalConns := stat.TotalConns()
	idleConns := stat.IdleConns()
	acquiredConns := stat.AcquiredConns()
	maxConns := stat.MaxConns()

	usagePercent := 0.0
	if maxConns > 0 {
		usagePercent = float64(acquiredConns) / float64(maxConns) * 100
	}

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │            CONNECTION POOL STATUS                    │
  ├──────────────────────────────────────────────────────┤
  │  Total connections    : %-29d │
  │  Idle connections     : %-29d │
  │  Acquired connections : %-29d │
  │  Max connections      : %-29d │
  │  Usage                : %-26.1f%% │
  └──────────────────────────────────────────────────────┘
`,
		totalConns,
		idleConns,
		acquiredConns,
		maxConns,
		usagePercent,
	)

	if usagePercent > 80 {
		fmt.Println("\n  ⚠️  Warning: Pool usage is high (>80%). Consider increasing DB_MAX_CONNS.")
	}
}

func recordHealthSnapshot(ctx context.Context, pool *pgxpool.Pool) {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("health_snapshot_%s.txt", timestamp)

	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not create snapshot file: %v\n", err))
		return
	}
	defer file.Close()

	fmt.Fprintf(file, "EVM Wallet Manager - Health Snapshot\n")
	fmt.Fprintf(file, "Generated: %s\n", time.Now().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(file, "========================================\n\n")

	// Run health check
	if err := core.RunHealthCheck(ctx, pool); err != nil {
		fmt.Fprintf(file, "[ERROR] Health check failed: %v\n", err)
	}

	// Add pool status
	fmt.Fprintf(file, "\nConnection Pool Status:\n")
	stat := pool.Stat()
	fmt.Fprintf(file, "  Total connections: %d\n", stat.TotalConns())
	fmt.Fprintf(file, "  Idle connections: %d\n", stat.IdleConns())
	fmt.Fprintf(file, "  Acquired connections: %d\n", stat.AcquiredConns())
	fmt.Fprintf(file, "  Max connections: %d\n", stat.MaxConns())
	fmt.Printf(core.Info("\n[INFO] Health snapshot saved to: %s\n", filename))
}

func showMaintenanceRecommendations(ctx context.Context, pool *pgxpool.Pool) {
	fmt.Println(core.Info("\n[INFO] Analyzing database for maintenance recommendations..."))

	// Check table bloat
	var walletCount int64
	err := pool.QueryRow(ct
x, `SELECT COUNT(*) FROM wallets`).Scan(&walletCount)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not count wallets: %v\n", err))
		return
	}

	// Check last vacuum
	var lastVacuum *time.Time
	err = pool.QueryRow(ctx, `
		SELECT last_vacuum 
		FROM pg_stat_user_tables 
		WHERE relname = 'wallets'
	`).Scan(&lastVacuum)
	if err != nil {
		fmt.Printf(core.Warning("[WARN] Could not check last vacuum time: %v\n", err))
	}

	// Check last analyze
	var lastAnalyze *time.Time
	err = pool.QueryRow(ctx, `
		SELECT last_analyze 
		FROM pg_stat_user_tables 
		WHERE relname = 'wallets'
	`).Scan(&lastAnalyze)
	if err != nil {
		fmt.Printf(core.Warning("[WARN] Could not check last analyze time: %v\n", err))
	}

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │         MAINTENANCE RECOMMENDATIONS                  │
  ├──────────────────────────────────────────────────────┤
  │  Total wallets: %-37d │
  └──────────────────────────────────────────────────────┘

  Recommendations:
`,
		walletCount,
	)

	// Provide recommendations
	if walletCount > 1000000 {
		fmt.Println("  • Consider running VACUUM ANALYZE on wallets table")
		fmt.Println("    Command: VACUUM ANALYZE wallets;")
	}

	if lastVacuum == nil || time.Since(*lastVacuum) > 7*24*time.Hour {
		fmt.Println("  • Wallets table hasn't been vacuumed recently")
		fmt.Println("    Run: VACUUM wallets;")
	}

	if lastAnalyze == nil || time.Since(*lastAnalyze) > 7*24*time.Hour {
		fmt.Println("  • Table statistics may be outdated")
		fmt.Println("    Run: ANALYZE wallets;")
	}

	if walletCount < 100000 {
		fmt.Println("  ✓ Database is in good health")
		fmt.Println("  ✓ No maintenance required at this time")
	}

	fmt.Println()
}

// ─── Monitoring Menu Helpers ──────────────────────────────────────────────────

func watchPoolStatusLive(ctx context.Context, pool *pgxpool.Pool, interval int) {
	fmt.Printf(core.Info("\n[INFO] Starting live pool status watch (press Enter to stop)...\n"))
	fmt.Printf("       Refreshing every %d seconds\n\n", interval)

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// Show initial status
	showPoolStatus(pool)

	// Create a channel to detect user interrupt
	done := make(chan bool)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		reader.ReadString('\n')
		done <- true
	}()

	for {
		select {
		case <-ctx.Done():
			fmt.Println(core.Info("\n[INFO] Watch stopped (context cancelled)"))
			return
		case <-done:
			fmt.Println(core.Info("\n[INFO] Watch stopped by user"))
			return
		case <-ticker.C:
			// Clear screen (ANSI escape code)
			fmt.Print("\033[H\033[2J")

			showPoolStatus(pool)
			fmt.Println(core.Info("\n[INFO] Press Enter to stop watching..."))
		}
	}
}

func watchWalletStatsLive(ctx context.Context, pool *pgxpool.Pool, interval int) {
	fmt.Printf(core.Info("\n[INFO] Starting live wallet stats watch (press Enter to stop)...\n"))
	fmt.Printf("       Refreshing every %d seconds\n\n", interval)

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// Show initial stats
	s, err := core.GetStats(ctx, pool)
	if err != nil {
		fmt.Printf(core.Error("[ERROR] Could not load stats: %v\n", err))
		return
	}
	core.PrintStats(s)

	// Create a channel to detect user interrupt
	done := make(chan bool)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		reader.ReadString('\n')
		done <- true
	}()

	for {
		select {
		case <-ctx.Done():
			fmt.Println(core.Info("\n[INFO] Watch stopped (context cancelled)"))
			return
		case <-done:
			fmt.Println(core.Info("\n[INFO] Watch stopped by user"))
			return
		case <-ticker.C:
			// Clear screen (ANSI escape code)
			fmt.Print("\033[H\033[2J")

			s, err := core.GetStats(ctx, pool)
			if err != nil {
				fmt.Printf(core.Error("[ERROR] Could not load stats: %v\n", err))
				return
			}
			core.PrintStats(s)
			fmt.Println(core.Info("\n[INFO] Press Enter to stop watching..."))
		}
	}
}

// ─── Benchmark Menu Helpers ───────────────────────────────────────────────────

func estimateSettings(cfg *config.Config) {
	// Estimate wallets per second based on typical performance
	// Typical: ~5000-10000 wallets/sec with 16 workers
	walletsPerSec := float64(cfg.Workers) * 625 // ~625 wallets/sec per worker

	scenarios := []struct {
		name  string
		count int
	}{
		{"Small run", 10000},
		{"Medium run", 100000},
		{"Large run", 1000000},
		{"Very large run", 10000000},
	}

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │           PERFORMANCE ESTIMATION                     │
  ├──────────────────────────────────────────────────────┤
  │  Current settings:                                   │
  │    Workers     : %-35d │
  │    Batch size  : %-35d │
  │    Estimated   : ~%.0f wallets/second%-15s │
  └──────────────────────────────────────────────────────┘

  Estimated completion times:
`, cfg.Workers, cfg.BatchSize, walletsPerSec, "")

	for _, s := range scenarios {
		seconds := float64(s.count) / walletsPerSec
		minutes := seconds / 60
		hours := minutes / 60

		timeStr := ""
		if hours >= 1 {
			timeStr = fmt.Sprintf("%.1f hours", hours)
		} else if minutes >= 1 {
			timeStr = fmt.Sprintf("%.1f minutes", minutes)
		} else {
			timeStr = fmt.Sprintf("%.0f seconds", seconds)
		}

		fmt.Printf("    %-20s : %s\n", s.name+" ("+formatNumber(s.count)+")", timeStr)
	}
	fmt.Println()
}

func runSmallBenchmark(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
	fmt.Println(core.Info("\n[INFO] Running small benchmark (1000 wallets)..."))
	fmt.Println("       This will measure actual performance on your system.\n")

	// Save original batch size
	originalBatchSize := cfg.BatchSize
	cfg.BatchSize = 100 // Use smaller batches for benchmark

	start := time.Now()

	if err := core.GenerateWallets(ctx, pool, cfg, 1000); err != nil {
		fmt.Printf(core.Error("[ERROR] Benchmark failed: %v\n", err))
		cfg.BatchSize = originalBatchSize
		return
	}

	elapsed := time.Since(start)
	walletsPerSec := 1000.0 / elapsed.Seconds()

	// Restore original batch size
	cfg.BatchSize = originalBatchSize

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │            BENCHMARK RESULTS                         │
  ├──────────────────────────────────────────────────────┤
  │  Wallets generated : 1,000                           │
  │  Time elapsed      : %-33s │
  │  Throughput        : ~%.0f wallets/second%-15s │
  │  Workers used      : %-33d │
  └──────────────────────────────────────────────────────┘
`, elapsed.Round(time.Millisecond), walletsPerSec, "", cfg.Workers)
}

func compareWorkerCounts(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
	fmt.Println(core.Info("\n[INFO] Comparing different worker counts..."))
	fmt.Println("       Testing with 500 wallets each\n")

	originalWorkers := cfg.Workers
	originalBatchSize := cfg.BatchSize
	cfg.BatchSize = 100

	workerCounts := []int{4, 8, 16, 32}
	results := make(map[int]time.Duration)

	for _, workers := range workerCounts {
		cfg.Workers = workers
		fmt.Printf("  Testing %d workers... ", workers)

		start := time.Now()
		if err := core.GenerateWallets(ctx, pool, cfg, 500); err != nil {
			fmt.Printf("failed: %v\n", err)
			continue
		}
		elapsed := time.Since(start)
		results[workers] = elapsed

		walletsPerSec := 500.0 / elapsed.Seconds()
		fmt.Printf("%.2fs (~%.0f wallets/sec)\n", elapsed.Seconds(), walletsPerSec)
	}

	// Restore original settings
	cfg.Workers = originalWorkers
	cfg.BatchSize = originalBatchSize

	fmt.Println("\n  Recommendation: Use the worker count with highest throughput")
	fmt.Println("                  that doesn't exceed your CPU capacity.\n")
}

func compareBatchSizes(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
	fmt.Println(core.Info("\n[INFO] Comparing different batch sizes..."))
	fmt.Println("       Testing with 1000 wallets each\n")

	originalBatchSize := cfg.BatchSize

	batchSizes := []int{100, 250, 500, 1000}
	results := make(map[int]time.Duration)

	for _, batchSize := range batchSizes {
		cfg.BatchSize = batchSize
		fmt.Printf("  Testing batch size %d... ", batchSize)

		start := time.Now()
		if err := core.GenerateWallets(ctx, pool, cfg, 1000); err != nil {
			fmt.Printf("failed: %v\n", err)
			continue
		}
		elapsed := time.Since(start)
		results[batchSize] = elapsed

		walletsPerSec := 1000.0 / elapsed.Seconds()
		fmt.Printf("%.2fs (~%.0f wallets/sec)\n", elapsed.Seconds(), walletsPerSec)
	}

	// Restore original settings
	cfg.BatchSize = originalBatchSize

	fmt.Println("\n  Recommendation: Larger batches are usually faster but use more memory.")
	fmt.Println("                  500-1000 is optimal for most systems.\n")
}

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// ─── Help Menu Pages ──────────────────────────────────────────────────────────

func showGenerationHelp() {
	fmt.Print(`
  ┌──────────────────────────────────────────────────────────────┐
  │                    GENERATION MODES                          │
  ├──────────────────────────────────────────────────────────────┤
  │                                                              │
  │  1. Generate by Wallet Count                                 │
  │     Enter the exact number of wallets you want to generate.  │
  │     Example: 10000 will generate exactly 10,000 wallets.     │
  │                                                              │
  │  2. Generate by Batch Count                                  │
  │     Enter number of batches. Each batch contains the         │
  │     configured batch size (default: 500 wallets).            │
  │     Example: 20 batches × 500 = 10,000 wallets               │
  │                                                              │
  │  Preview & Confirmation:                                     │
  │     - All runs show a preview before starting                │
  │     - Large runs (>10,000 wallets) require confirmation      │
  │     - You can cancel before generation starts                │
  │                                                              │
  │  Best Practices:                                             │
  │     - Start with small runs (1,000-10,000) to test           │
  │     - Monitor system resources during generation             │
  │     - Use batch mode for predictable resource usage          │
  │     - Check database space before very large runs            │
  │                                                              │
  └──────────────────────────────────────────────────────────────┘

  Press Enter to continue...
`)
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func showBatchSizeHelp() {
	fmt.Print(`
  ┌──────────────────────────────────────────────────────────────┐
  │                     BATCH SIZE GUIDE                         │
  ├──────────────────────────────────────────────────────────────┤
  │                                                              │
  │  What is Batch Size?                                         │
  │     Number of wallets inserted into the database in a        │
  │     single transaction. Larger batches = fewer transactions. │
  │                                                              │
  │  Recommended Values:                                         │
  │     • Small systems (2-4 GB RAM):  100-250 wallets           │
  │     • Medium systems (8-16 GB RAM): 500 wallets (default)    │
  │     • Large systems (32+ GB RAM):   1000 wallets             │
  │                                                              │
  │  Trade-offs:                                                 │
  │     Larger batches:                                          │
  │       ✓ Faster overall throughput                            │
  │       ✓ Fewer database round-trips                           │
  │       ✗ Higher memory usage                                  │
  │       ✗ Longer rollback time on failure                      │
  │                                                              │
  │     Smaller batches:                                         │
  │       ✓ Lower memory usage                                   │
  │       ✓ Faster failure recovery                              │
  │       ✗ More database overhead                               │
  │       ✗ Slightly slower overall                              │
  │                                                              │
  │  Limits:                                                     │
  │     Minimum: 1 wallet                                        │
  │     Maximum: 1000 wallets (PostgreSQL COPY limit)            │
  │                                                              │
  └──────────────────────────────────────────────────────────────┘

  Press Enter to continue...
`)
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func showWorkersHelp() {
	fmt.Print(`
  ┌──────────────────────────────────────────────────────────────┐
  │                      WORKERS GUIDE                           │
  ├──────────────────────────────────────────────────────────────┤
  │                                                              │
  │  What are Workers?                                           │
  │     Parallel goroutines that generate wallet key pairs.      │
  │     More workers = higher throughput (up to a point).        │
  │                                                              │
  │  Recommended Values:                                         │
  │     • 2-4 CPU cores:   4-8 workers                           │
  │     • 4-8 CPU cores:   8-16 workers (default: 16)            │
  │     • 8-16 CPU cores:  16-32 workers                         │
  │     • 16+ CPU cores:   32-64 workers                         │
  │                                                              │
  │  How to Choose:                                              │
  │     1. Start with 2× your CPU core count                     │
  │     2. Run a small benchmark (Benchmark menu)                │
  │     3. Increase workers until throughput plateaus            │
  │     4. Monitor CPU usage - should be 70-90%                  │
  │                                                              │
  │  Trade-offs:                                                 │
  │     More workers:                                            │
  │       ✓ Higher throughput (up to CPU limit)                  │
  │       ✗ Higher CPU usage                                     │
  │       ✗ Higher memory usage                                  │
  │       ✗ Diminishing returns beyond optimal point             │
  │                                                              │
  │  Limits:                                                     │
  │     Minimum: 1 worker                                        │
  │     Maximum: 100 workers (configurable limit)                │
  │     Optimal: Usually 8-32 for most systems                   │
  │                                                              │
  └──────────────────────────────────────────────────────────────┘

  Press Enter to continue...
`)
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func showDatabaseHelp() {
	fmt.Print(`
  ┌──────────────────────────────────────────────────────────────┐
  │                     DATABASE GUIDE                           │
  ├──────────────────────────────────────────────────────────────┤
  │                                                              │
  │  Connection Pool:                                            │
  │     The application maintains a pool of database             │
  │     connections for efficient batch inserts.                 │
  │                                                              │
  │  Pool Settings:                                              │
  │     • Max connections: 30 (default)                          │
  │     • Min connections: 5 (default)                           │
  │     • Configure via DB_MAX_CONNS and DB_MIN_CONNS            │
  │                                                              │
  │  Health Checks:                                              │
  │     Use "Database tools" menu to:                            │
  │     • Check database connectivity                            │
  │     • Monitor connection pool status                         │
  │     • Record health snapshots                                │
  │     • Get maintenance recommendations                        │
  │                                                              │
  │  Maintenance:                                                │
  │     For large databases (>1M wallets):                       │
  │     • Run VACUUM ANALYZE periodically                        │
  │     • Monitor table bloat                                    │
  │     • Update statistics regularly                            │
  │     • Check index health                                     │
  │                                                              │
  │  Storage Requirements:                                       │
  │     Approximate size per wallet: ~100 bytes                  │
  │     • 1 million wallets   ≈ 100 MB                           │
  │     • 10 million wallets  ≈ 1 GB                             │
  │     • 100 million wallets ≈ 10 GB                            │
  │                                                              │
  │  Performance Tips:                                           │
  │     • Use SSD storage for best performance                   │
  │     • Ensure adequate disk space (2× expected size)          │
  │     • Monitor connection pool usage                          │
  │     • Keep PostgreSQL updated                                │
  │                                                              │
  └──────────────────────────────────────────────────────────────┘

  Press Enter to continue...
`)
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func showSettingsHelp() {
	fmt.Print(`
  ┌──────────────────────────────────────────────────────────────┐
  │                     SETTINGS GUIDE                           │
  ├──────────────────────────────────────────────────────────────┤
  │                                                              │
  │  Configuration Menu:                                         │
  │     Access via main menu option 6 to modify runtime          │
  │     settings without editing .env file.                      │
  │                                                              │
  │  Available Settings:                                         │
  │                                                              │
  │  1. Workers (1-100)                                          │
  │     Number of parallel wallet generators.                    │
  │     Default: 16                                              │
  │     Recommended: 8-32 for most systems                       │
  │                                                              │
  │  2. Batch Size (1-1000)                                      │
  │     Wallets per database transaction.                        │
  │     Default: 500                                             │
  │     Recommended: 500-1000 for best performance               │
  │                                                              │
  │  3. Logging (enable/disable)                                 │
  │     Toggle batch completion logs.                            │
  │     Default: enabled                                         │
  │     Note: Errors/warnings always shown                       │
  │                                                              │
  │  4. Pool Monitor Interval (0-300 seconds)                    │
  │     How often to log pool statistics.                        │
  │     Default: 30 seconds                                      │
  │     Set to 0 to disable monitoring                           │
  │                                                              │
  │  5. Pool Warning Threshold (0.1-1.0)                         │
  │     Connection usage ratio that triggers warnings.           │
  │     Default: 0.8 (80%)                                       │
  │     Recommended: 0.7-0.9                                     │
  │                                                              │
  │  Session vs Permanent:                                       │
  │     • All changes apply to current session only              │
  │     • Settings reset when application restarts               │
  │     • Use "Reset session settings" to restore defaults       │
  │     • Edit .env file for permanent changes                   │
  │                                                              │
  └──────────────────────────────────────────────────────────────┘

  Press Enter to continue...
`)
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// ─── First-Run Tips ───────────────────────────────────────────────────────────

func showFirstRunTips(reader *bufio.Reader, cfg *config.Config) {
	fmt.Printf(`
  ╔══════════════════════════════════════════════════════════════╗
  ║                    WELCOME TO EVM WALLET MANAGER             ║
  ╠══════════════════════════════════════════════════════════════╣
  ║                                                              ║
  ║  Quick Start Tips:                                           ║
  ║                                                              ║
  ║  1. Start Small                                              ║
  ║     Try generating 1,000-10,000 wallets first to test       ║
  ║     your system's performance.                               ║
  ║                                                              ║
  ║  2. Use Letter Shortcuts                                     ║
  ║     Press 'G' for Generate, 'S' for Stats, 'Q' to Quit      ║
  ║     Much faster than typing numbers!                         ║
  ║                                                              ║
  ║  3. Monitor Performance                                      ║
  ║     Check Statistics (S) to see generation speed             ║
  ║     Use Benchmark (B) to optimize your settings              ║
  ║                                                              ║
  ║  4. Current Settings                                         ║
  ║     Workers: %-2d  |  Batch Size: %-4d  |  UI: %-8s    ║
  ║                                                              ║
  ║  5. Get Help Anytime                                         ║
  ║     Press 'H' from main menu for detailed guides             ║
  ║                                                              ║
  ║  Tip: You can disable these tips in Configuration menu      ║
  ║                                                              ║
  ╚══════════════════════════════════════════════════════════════╝

  %s
`, cfg.Workers, cfg.BatchSize, cfg.UIMode, core.Hint("Press Enter to continue..."))

	readLine(reader)
	core.ClearScreenIfEnabled()
}

// isMinimalUI returns true if UI mode is set to minimal
func isMinimalUI(cfg *config.Config) bool {
	return cfg.UIMode == "minimal"
}
