package internal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// Interactive CLI Menu
// ============================================================================

// Run is the main entry point for the interactive CLI.
func Run(ctx context.Context, store Storage, cfg *Config) {
	reader := bufio.NewReader(os.Stdin)

	// Clear screen and show banner on startup
	ClearScreenIfEnabled()
	printBanner()

	for {
		// Check if context is cancelled (graceful shutdown)
		select {
		case <-ctx.Done():
			fmt.Println(Info("\n[INFO] Shutdown requested, exiting..."))
			return
		default:
		}

		// Show status strip
		if strip, err := GetStatusStrip(ctx, store, cfg); err == nil {
			strip.Render()
		}

		printMenu(cfg)
		choice := normalizeMenuChoice(readLine(reader))

		// Clear screen before handling choice
		ClearScreenIfEnabled()

		switch choice {
		case "1":
			handleGenerateMenu(ctx, store, cfg, reader)
		case "2":
			handleVanityMenuNew(ctx, store, cfg, reader)
		case "3":
			handleStatsMenu(ctx, store, reader)
		case "4":
			handleConfigMenu(cfg, reader)
		case "5":
			handleBenchmarkMenu(ctx, store, cfg, reader)
		case "6":
			handleImportVerify(reader)
		case "7":
			handleHelpMenu(reader)
		case "0":
			fmt.Println(Info("\n[INFO] Goodbye."))
			return
		default:
			fmt.Println(Warning("\n[WARN] Invalid option — please choose 1-7 or 0."))
		}
	}
}

// ============================================================================
// Status Strip
// ============================================================================

// StatusStrip displays current system state on the home screen.
type StatusStrip struct {
	WalletCount int64
	LastRunRate int
	StorageInfo string
}

// GetStatusStrip retrieves current system status from storage.
func GetStatusStrip(ctx context.Context, store Storage, cfg *Config) (*StatusStrip, error) {
	count, err := store.CountWallets(ctx)
	if err != nil {
		return nil, err
	}

	label := store.StorageType()
	if cfg.StorageType == "postgres" {
		label = fmt.Sprintf("postgres (%s)", cfg.DBName)
	} else if cfg.DataDir != "" {
		label = fmt.Sprintf("sqlite (%s)", cfg.DataDir)
	}

	return &StatusStrip{
		WalletCount: count,
		LastRunRate: 0,
		StorageInfo: label,
	}, nil
}

// Render displays the status strip with color coding.
func (s *StatusStrip) Render() {
	if !IsColorEnabled() {
		fmt.Printf("Status: %s wallets in %s\n\n", FormatNumber(int(s.WalletCount)), s.StorageInfo)
		return
	}

	status := Success("READY")
	walletInfo := fmt.Sprintf("%s wallets in %s",
		FormatNumber(int(s.WalletCount)), s.StorageInfo)

	lastRun := Hint("no recent runs")
	if s.LastRunRate > 0 {
		lastRun = fmt.Sprintf("last run %s/s", FormatNumber(s.LastRunRate))
	}

	fmt.Printf("\n   %s   ·   %s   ·   %s\n\n", status, walletInfo, lastRun)
}

// ============================================================================
// Menu Handlers
// ============================================================================

func handleGenerateMenu(ctx context.Context, store Storage, cfg *Config, reader *bufio.Reader) {
	// Flattened: Direct inline prompts instead of submenu
	ClearScreenIfEnabled()

	fmt.Printf("\n%s\n", Highlight("GENERATE WALLETS"))
	fmt.Printf("  Current settings: %d workers, batch size %d\n\n", cfg.Workers, cfg.BatchSize)

	fmt.Print("  Enter wallet count (or 'b' for batch mode, 'h' for HD/seed phrase, 's' for settings): ")
	input := strings.ToLower(strings.TrimSpace(readLine(reader)))

	if input == "" {
		return
	}

	if input == "s" || input == "settings" {
		changeGenerationSettings(cfg, reader)
		handleGenerateMenu(ctx, store, cfg, reader) // Recursive call to show menu again
		return
	}

	if input == "h" || input == "hd" || input == "seed" {
		generateFromSeedPhrase(ctx, store, cfg, reader)
		return
	}

	if input == "b" || input == "batch" {
		// Batch mode
		fmt.Printf("\n  Enter batch count (1 batch = %d wallets): ", cfg.BatchSize)
		batchInput := strings.TrimSpace(readLine(reader))

		batches, err := strconv.Atoi(batchInput)
		if err != nil || batches < 1 {
			fmt.Println(Error("\n[ERROR] Please enter a valid positive number for batches."))
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
			return
		}

		total, ok := generationTotal(batches, cfg.BatchSize)
		if !ok {
			fmt.Print(Error("\n[ERROR] Total wallets exceeds maximum safe value.\n"))
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
			return
		}

		fmt.Print(Info("\n[INFO] %d batches × %d wallets = %s wallets\n", batches, cfg.BatchSize, FormatNumber(total)))
		generateWallets(ctx, store, cfg, total, reader)
		return
	}

	// Direct count mode
	count, err := strconv.Atoi(input)
	if err != nil || count < 1 {
		fmt.Println(Error("\n[ERROR] Please enter a valid positive number."))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	generateWallets(ctx, store, cfg, count, reader)
}

func handleStatsMenu(ctx context.Context, store Storage, reader *bufio.Reader) {
	// Flattened: Show stats immediately with action options
	ClearScreenIfEnabled()

	// Show stats immediately
	handleStats(ctx, store)

	// Offer quick actions
	fmt.Printf("\n  %s refresh   %s back\n",
		Hint("[R]"), Hint("[Enter]"))
	fmt.Print("  Action: ")

	choice := strings.ToLower(strings.TrimSpace(readLine(reader)))

	switch choice {
	case "r", "refresh":
		handleStatsMenu(ctx, store, reader) // Recursive refresh
	case "":
		return // Back to main menu
	default:
		return
	}
}

func handleVanityMenuNew(ctx context.Context, store Storage, cfg *Config, reader *bufio.Reader) {
	ClearScreenIfEnabled()

	fmt.Printf("\n%s\n", Highlight("═══════════════════════════════════════════════════════════════"))
	fmt.Printf("%s\n", Highlight("  VANITY ADDRESS GENERATOR"))
	fmt.Printf("%s\n\n", Highlight("═══════════════════════════════════════════════════════════════"))

	fmt.Println(Info("  Generate custom Ethereum addresses with specific patterns"))
	fmt.Println()

	// Step 1: Prefix input with validation loop
	fmt.Println(Highlight("  Step 1: Address Prefix (after 0x)"))
	fmt.Println()
	fmt.Println(Hint("  • Leave empty to skip prefix matching"))
	fmt.Println(Hint("  • Valid characters: 0-9, a-f (hex only)"))
	fmt.Println(Hint("  • Example: 'dead' will match 0xdead..."))
	fmt.Println()

	var prefix string
	for {
		fmt.Print("  Enter prefix (or press Enter to skip): ")
		prefix = strings.TrimSpace(strings.ToLower(readLine(reader)))

		if prefix == "" {
			fmt.Printf("  %s No prefix - will match any start\n", Info("ℹ"))
			break
		}

		if err := ValidateVanityPattern(prefix, "prefix"); err != nil {
			fmt.Printf("\n  %s Invalid prefix '%s'\n", Error("✗"), prefix)
			fmt.Printf("  %s %v\n", Hint("→"), err)
			fmt.Println(Hint("  Valid characters: 0-9, a-f (lowercase hex only)"))
			fmt.Println(Hint("  Example: 'dead', 'cafe', '1234'"))
			fmt.Println()
			continue
		}

		fmt.Printf("  %s Prefix set: 0x%s...\n", Success("✓"), prefix)
		break
	}

	// Step 2: Suffix input with validation loop
	fmt.Println()
	fmt.Println(Highlight("  Step 2: Address Suffix (end of address)"))
	fmt.Println()
	fmt.Println(Hint("  • Leave empty to skip suffix matching"))
	fmt.Println(Hint("  • Valid characters: 0-9, a-f (hex only)"))
	fmt.Println(Hint("  • Example: 'beef' will match ...beef"))
	fmt.Println()

	var suffix string
	for {
		fmt.Print("  Enter suffix (or press Enter to skip): ")
		suffix = strings.TrimSpace(strings.ToLower(readLine(reader)))

		if suffix == "" {
			fmt.Printf("  %s No suffix - will match any end\n", Info("ℹ"))
			break
		}

		if err := ValidateVanityPattern(suffix, "suffix"); err != nil {
			fmt.Printf("\n  %s Invalid suffix '%s'\n", Error("✗"), suffix)
			fmt.Printf("  %s %v\n", Hint("→"), err)
			fmt.Println(Hint("  Valid characters: 0-9, a-f (lowercase hex only)"))
			fmt.Println(Hint("  Example: 'beef', 'dead', '9999'"))
			fmt.Println()
			continue
		}

		fmt.Printf("  %s Suffix set: ...%s\n", Success("✓"), suffix)
		break
	}

	// Validate at least one pattern
	if prefix == "" && suffix == "" {
		fmt.Println(Warning("\n[WARN] No pattern specified (both prefix and suffix are empty)"))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	// Show preview of matching address
	fmt.Println()
	fmt.Println(Highlight("  Preview of matching address:"))
	previewAddr := generatePreviewAddress(prefix, suffix)
	fmt.Printf("  %s\n", Info(previewAddr))
	fmt.Println()

	// Step 3: Case sensitivity
	fmt.Println()
	fmt.Println(Highlight("  Step 3: Case Sensitivity"))
	fmt.Println(Info("  • Case-insensitive (default): Faster, easier to find"))
	fmt.Println(Info("  • Case-sensitive (EIP-55): Slower, exact match required"))
	fmt.Println()
	fmt.Println(Hint("  Example with prefix 'ABC':"))
	fmt.Println(Hint("    Case-insensitive: 0xabc..., 0xABC..., 0xAbC... all match"))
	fmt.Println(Hint("    Case-sensitive:   Only 0xABC... matches"))
	fmt.Print("\n  Use case-sensitive matching? [y/N]: ")
	checksumInput := strings.ToLower(strings.TrimSpace(readLine(reader)))
	checksum := checksumInput == "y" || checksumInput == "yes"

	if checksum {
		fmt.Printf("  %s Case-sensitive mode enabled (EIP-55 checksum)\n", Warning("⚠"))
	} else {
		fmt.Printf("  %s Case-insensitive mode (faster)\n", Success("✓"))
	}

	// Create pattern
	patterns := []VanityPattern{{
		Prefix: prefix,
		Suffix: suffix,
		Name:   fmt.Sprintf("%s...%s", prefix, suffix),
	}}

	// Calculate and show difficulty
	difficulty := CalculateMultiPatternDifficulty(patterns, checksum)
	speed := CalibrateSpeed(ctx, cfg)
	time50, time99 := EstimateTime(difficulty, speed)

	fmt.Println()
	fmt.Println(Highlight("  ─────────────────────────────────────────────────────────────"))
	fmt.Printf("  %s Pattern: 0x%s...%s\n", Info("ℹ"), prefix, suffix)
	fmt.Printf("  %s Difficulty: %s\n", Info("ℹ"), FormatDifficulty(difficulty))
	fmt.Printf("  %s Speed: ~%.0f addr/s\n", Info("ℹ"), speed)
	fmt.Printf("  %s Estimated time per match:\n", Info("ℹ"))
	fmt.Printf("      50%% chance: %s\n", FormatDuration(time50))
	fmt.Printf("      99%% chance: %s\n", FormatDuration(time99))
	fmt.Println(Highlight("  ─────────────────────────────────────────────────────────────"))

	// Step 4: Number of wallets
	fmt.Println()
	fmt.Println(Highlight("  Step 4: Number of Matching Wallets"))
	fmt.Println(Hint("  • How many wallets with this pattern do you want?"))
	fmt.Println(Hint("  • If more than 5, only first 5 will be shown in terminal"))
	fmt.Println(Hint("  • All wallets will be saved to database"))
	
	var count int
	for {
		countVal, ok := promptPositiveInt(reader, "\n  Number of wallets to generate: ")
		if !ok {
			continue
		}
		count = countVal
		break
	}

	if count > 5 {
		fmt.Printf("  %s Will generate %d wallets (showing first 5 in terminal)\n", Info("ℹ"), count)
	}

	// Step 5: Confirm generation
	fmt.Println()
	fmt.Println(Highlight("  ─────────────────────────────────────────────────────────────"))
	fmt.Println(Success("  Ready to generate!"))
	fmt.Printf("  Pattern: 0x%s...%s (%s)\n", prefix, suffix, 
		map[bool]string{true: "case-sensitive", false: "case-insensitive"}[checksum])
	fmt.Printf("  Target: %d wallet(s)\n", count)
	fmt.Println(Highlight("  ─────────────────────────────────────────────────────────────"))
	fmt.Print("\n  Start generation? [Y/n]: ")
	confirmInput := strings.ToLower(strings.TrimSpace(readLine(reader)))
	if confirmInput == "n" || confirmInput == "no" {
		fmt.Println(Warning("\n[CANCELLED] Generation cancelled by user"))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	// Create vanity config
	vanityConfig := VanityConfig{
		Patterns:    patterns,
		Checksum:    checksum,
		TargetCount: count,
	}

	// Generate vanity wallets
	fmt.Println()
	if err := GenerateVanityWallets(ctx, store, cfg, vanityConfig); err != nil {
		fmt.Print(Error("\n[ERROR] Vanity generation failed: %v\n", err))
	}

	fmt.Print("\n  Press Enter to continue...")
	readLine(reader)
}

func handleBenchmarkMenu(ctx context.Context, store Storage, cfg *Config, reader *bufio.Reader) {
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
			runSmallBenchmark(ctx, store, cfg)
		case "3":
			compareWorkerCounts(ctx, store, cfg)
		case "4":
			compareBatchSizes(ctx, store, cfg)
		case "5":
			return
		default:
			fmt.Println(Warning("\n[WARN] Invalid option — please choose 1 to 5."))
		}
	}
}

func handleConfigMenu(cfg *Config, reader *bufio.Reader) {
	// Store original config for reset functionality
	originalCfg := *cfg

	for {
		loggingStatus := "enabled"
		if !cfg.EnableLogging {
			loggingStatus = "disabled"
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
  ├──────────────────────────────────────┤
  │   1   Show current settings          │
  │   2   Workers                        │
  │   3   Batch size                     │
  │   4   Logging (enable/disable)       │
  │   5   Pool monitor interval          │
  │   6   Pool warning threshold         │
  │   7   UI mode (full/minimal)         │
  │   8   Reset session settings         │
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
			resetSessionSettings(cfg, &originalCfg, reader)
		case "0":
			return
		default:
			fmt.Println(Warning("\n[WARN] Invalid option — please choose 1-8 or 0."))
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
  │   4   Settings guide                       │
  │   5   Back                                 │
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
			showSettingsHelp()
		case "5":
			return
		default:
			fmt.Println(Warning("\n[WARN] Invalid option — please choose 1 to 5."))
		}
	}
}

func handleImportVerify(reader *bufio.Reader) {
	ClearScreenIfEnabled()

	fmt.Printf("\n%s\n", Highlight("IMPORT & VERIFY PRIVATE KEY"))
	fmt.Println("  Paste a private key to verify and see its address.")
	fmt.Println("  The key is NOT saved to the database.")
	fmt.Println()

	// Prompt for private key
	fmt.Print("  Private key (with or without 0x prefix): ")
	privateKeyInput := strings.TrimSpace(readLine(reader))

	if privateKeyInput == "" {
		fmt.Println(Warning("\n[WARN] No private key provided."))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	// Import and verify the private key
	result := ImportPrivateKey(privateKeyInput)

	// Clear screen and display result
	ClearScreenIfEnabled()
	fmt.Printf("\n%s\n", Highlight("IMPORT & VERIFY RESULT"))
	fmt.Println()

	if !result.Valid {
		// Display error
		fmt.Printf("  %s Invalid private key\n", Error("✗"))
		fmt.Printf("  %s %s\n\n", Error("Error:"), result.Error)
	} else {
		// Display success
		fmt.Printf("  %s Valid private key\n\n", Success("✓"))

		// Display normalized private key
		fmt.Printf("  %s\n", Highlight("Private Key (normalized):"))
		fmt.Printf("  %s\n\n", result.PrivateKey)

		// Display derived address (EIP-55 checksum)
		fmt.Printf("  %s\n", Highlight("Derived Address (EIP-55):"))
		fmt.Printf("  %s\n\n", result.Address)

		// Show verification info
		fmt.Printf("  %s This address was derived from the provided private key.\n", Info("ℹ"))
		fmt.Printf("  %s The key was NOT saved to the database.\n", Info("ℹ"))
		fmt.Println()
	}

	fmt.Print("  Press Enter to continue...")
	readLine(reader)
}

func handleStats(ctx context.Context, store Storage) {
	fmt.Println(Info("\n[INFO] Loading statistics..."))

	s, err := GetStats(ctx, store)
	if err != nil {
		fmt.Print(Error("[ERROR] Could not load stats: %v\n", err))
		return
	}
	PrintStats(s)
}

// ============================================================================
// HD Wallet Generation
// ============================================================================

// generateFromSeedPhrase handles the HD wallet generation menu flow
func generateFromSeedPhrase(ctx context.Context, store Storage, cfg *Config, reader *bufio.Reader) {
	fmt.Println()
	fmt.Println(Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println(Highlight("  HD WALLET GENERATION (BIP-39/BIP-44)"))
	fmt.Println(Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println()

	// Step 1: Choose to generate new or use existing mnemonic
	fmt.Println("Options:")
	fmt.Println("  1. Generate new seed phrase (12 words)")
	fmt.Println("  2. Generate new seed phrase (24 words)")
	fmt.Println("  3. Use existing seed phrase")
	fmt.Println("  0. Back to main menu")
	fmt.Println()
	fmt.Print("Choose option: ")

	choice := strings.TrimSpace(readLine(reader))

	var mnemonic string
	var err error

	switch choice {
	case "1":
		// Generate 12-word mnemonic
		mnemonic, err = GenerateMnemonic(Mnemonic12Words)
		if err != nil {
			fmt.Println(Error("[ERROR] Failed to generate mnemonic: %v", err))
			return
		}
		displayMnemonic(mnemonic, 12)

	case "2":
		// Generate 24-word mnemonic
		mnemonic, err = GenerateMnemonic(Mnemonic24Words)
		if err != nil {
			fmt.Println(Error("[ERROR] Failed to generate mnemonic: %v", err))
			return
		}
		displayMnemonic(mnemonic, 24)

	case "3":
		// Use existing mnemonic
		fmt.Println()
		fmt.Println("Enter your BIP-39 seed phrase (12 or 24 words):")
		fmt.Print("> ")
		mnemonic = strings.TrimSpace(readLine(reader))

		// Validate mnemonic
		if err := ValidateMnemonic(mnemonic); err != nil {
			fmt.Println(Error("[ERROR] Invalid seed phrase: %v", err))
			fmt.Println("       Please check your seed phrase and try again.")
			return
		}
		fmt.Println(Success("[✓] Seed phrase validated successfully"))

	case "0":
		return

	default:
		fmt.Println(Error("[ERROR] Invalid option"))
		return
	}

	// Step 2: Optional passphrase with detailed explanation
	fmt.Println()
	fmt.Println(Highlight("═══════════════════════════════════════════════════════════════"))
	fmt.Println(Highlight("  BIP-39 PASSPHRASE (Optional Security Feature)"))
	fmt.Println(Highlight("═══════════════════════════════════════════════════════════════"))
	fmt.Println()
	fmt.Println(Info("  What is it?"))
	fmt.Println("  • An extra \"25th word\" added to your seed phrase")
	fmt.Println("  • Creates completely different wallets from the same mnemonic")
	fmt.Println("  • Acts as a second authentication factor")
	fmt.Println()
	fmt.Println(Success("  ✓ Benefits:"))
	fmt.Println("    - Enhanced security: Protects funds even if mnemonic is stolen")
	fmt.Println("    - Plausible deniability: Multiple wallets from one mnemonic")
	fmt.Println()
	fmt.Println(Warning("  ⚠ Critical Warnings:"))
	fmt.Println("    - If you forget the passphrase, funds are PERMANENTLY LOST")
	fmt.Println("    - Different passphrase = different wallets (case-sensitive)")
	fmt.Println("    - \"MyPass\" ≠ \"mypass\" ≠ \"MyPass \" (with space)")
	fmt.Println()
	fmt.Println(Hint("  Recommendation:"))
	fmt.Println("  • Press Enter to skip (simpler, most users)")
	fmt.Println("  • Use passphrase only if you need maximum security")
	fmt.Println("  • Store passphrase separately from mnemonic if used")
	fmt.Println()
	fmt.Println(Highlight("═══════════════════════════════════════════════════════════════"))
	fmt.Println()
	fmt.Print("Enter passphrase (or press Enter to skip): ")
	passphrase := strings.TrimSpace(readLine(reader))

	if passphrase != "" {
		fmt.Println()
		fmt.Println(Warning("[⚠] Passphrase set - you MUST remember this to access these wallets"))
		fmt.Println(Info("[ℹ] This will generate different addresses than without passphrase"))
	} else {
		fmt.Println()
		fmt.Println(Success("[✓] No passphrase - using standard derivation"))
	}

	// Step 3: Number of addresses to derive
	fmt.Println()
	fmt.Print("How many addresses to derive? (1-100): ")
	countStr := strings.TrimSpace(readLine(reader))

	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 || count > 100 {
		fmt.Println(Error("[ERROR] Invalid count. Must be between 1 and 100"))
		return
	}

	// Step 4: Derive wallets
	fmt.Println()
	fmt.Println(Info("[INFO] Deriving %d addresses from seed phrase...", count))

	hdConfig := HDWalletConfig{
		Mnemonic:   mnemonic,
		Passphrase: passphrase,
		StartIndex: 0,
		Count:      uint32(count),
	}

	wallets, err := DeriveWalletsFromMnemonic(hdConfig)
	if err != nil {
		fmt.Println(Error("[ERROR] Failed to derive wallets: %v", err))
		return
	}

	// Step 5: Display derived wallets
	fmt.Println()
	fmt.Println(Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println(Highlight("  DERIVED WALLETS (BIP-44: m/44'/60'/0'/0/i)"))
	fmt.Println(Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println()

	for i, w := range wallets {
		fmt.Printf("  [%d] %s\n", i, Highlight("m/44'/60'/0'/0/%d", i))
		fmt.Printf("      Address:     0x%s\n", w.AddressHex())
		fmt.Printf("      Private Key: 0x%s\n", w.PrivateKeyHex())
		fmt.Println()
	}

	// Step 6: Save to database
	fmt.Print("Save these wallets to database? (y/n): ")
	confirm := strings.TrimSpace(strings.ToLower(readLine(reader)))

	if confirm != "y" && confirm != "yes" {
		fmt.Println(Info("[INFO] Wallets not saved"))
		return
	}

	// Save wallets
	ids, err := store.SaveWallets(ctx, wallets)
	if err != nil {
		fmt.Println(Error("[ERROR] Failed to save wallets: %v", err))
		return
	}

	fmt.Println()
	fmt.Println(Success("[✓] Successfully saved %d HD wallets to database", len(ids)))

	// Step 7: Export option
	if cfg.ExportEnabled {
		fmt.Println()
		fmt.Print("Export wallets to file? (y/n): ")
		exportConfirm := strings.TrimSpace(strings.ToLower(readLine(reader)))

		if exportConfirm == "y" || exportConfirm == "yes" {
			exporter, err := NewExporter(*cfg)
			if err != nil {
				fmt.Println(Error("[ERROR] Failed to initialize exporter: %v", err))
				return
			}
			defer exporter.Close()

			for _, w := range wallets {
				if err := exporter.Export(w.AddressHex(), "0x"+w.PrivateKeyHex()); err != nil {
					fmt.Println(Error("[WARN] Export failed for wallet: %v", err))
				}
			}
			fmt.Println(Success("[✓] Wallets exported to %s", cfg.ExportDir))
		}
	}
}

// displayMnemonic shows the generated mnemonic with a strong warning
func displayMnemonic(mnemonic string, wordCount int) {
	fmt.Println()
	fmt.Println(Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println(Highlight("  YOUR %d-WORD SEED PHRASE", wordCount))
	fmt.Println(Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println()
	fmt.Println(Highlight("  %s", mnemonic))
	fmt.Println()
	fmt.Println(Error("  ⚠️  CRITICAL SECURITY WARNING ⚠️"))
	fmt.Println()
	fmt.Println("  • Write this seed phrase down on paper")
	fmt.Println("  • Store it in a secure location (safe, vault)")
	fmt.Println("  • NEVER share it with anyone")
	fmt.Println("  • NEVER store it digitally (no screenshots, no cloud)")
	fmt.Println("  • Anyone with this phrase can access ALL derived wallets")
	fmt.Println("  • Loss of this phrase = permanent loss of funds")
	fmt.Println()
	fmt.Print("Press Enter after you have securely backed up your seed phrase...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// ============================================================================
// Helper Functions
// ============================================================================

func printBanner() {
	const innerW = 55

	title := Highlight("EVM WALLET MANAGER") + " v1.0"
	subtitle := Hint("Multi-chain · Embedded SQLite · Go")
	chains := Info("ETH · BSC · Polygon · Arbitrum · Optimism · Base")

	fmt.Println(BoxTop(innerW))
	fmt.Println(BoxRow(innerW, "", 0))
	fmt.Println(BoxRow(innerW, title, 0))
	fmt.Println(BoxRow(innerW, subtitle, 0))
	fmt.Println(BoxRow(innerW, "", 0))
	fmt.Println(BoxRow(innerW, chains, 0))
	fmt.Println(BoxRow(innerW, "", 0))
	fmt.Println(BoxBottom(innerW))
}

// normalizeMenuChoice converts letter shortcuts to number choices.
func normalizeMenuChoice(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))

	// Letter shortcuts for main menu
	shortcuts := map[string]string{
		"g": "1", // Generate
		"v": "2", // Vanity
		"s": "3", // Statistics
		"c": "4", // Configuration
		"b": "5", // Benchmark
		"i": "6", // Import & verify
		"h": "7", // Help
		"q": "0", // Quit
		"x": "0", // Alternative quit
	}

	if mapped, ok := shortcuts[input]; ok {
		return mapped
	}
	return input
}

func printMenu(cfg *Config) {
	title := Highlight("MAIN MENU")

	// Generate hint showing current settings
	genHint := Hint("batch %d", cfg.BatchSize)
	settingsHint := Hint("%d workers", cfg.Workers)

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
		Success("1"), Hint("[G]"), genHint,
		Success("2"), Hint("[V]"),
		Info("3"), Hint("[S]"),
		Info("4"), Hint("[C]"), settingsHint,
		Info("5"), Hint("[B]"),
		Info("6"), Hint("[I]"),
		Info("7"), Hint("[H]"),
		Warning("0"), Hint("[Q]"),
		Hint("Select option:"))
}

func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

func promptPositiveInt(reader *bufio.Reader, prompt string) (int, bool) {
	fmt.Print(prompt)
	input := strings.TrimSpace(readLine(reader))

	if input == "" {
		fmt.Println(Error("\n[ERROR] Input cannot be empty."))
		fmt.Println("        Please enter a positive integer (e.g., 1, 5, 100).")
		return 0, false
	}

	n, err := strconv.Atoi(input)
	if err != nil {
		fmt.Print(Error("\n[ERROR] Invalid input: '%s' is not a valid number.\n", input))
		fmt.Println("        Please enter a positive integer (e.g., 1, 5, 100).")
		fmt.Println("        Examples: 1000, 50000, 1000000")
		return 0, false
	}

	if n < 1 {
		fmt.Print(Error("\n[ERROR] Invalid value: %d is not positive.\n", n))
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

func changeGenerationSettings(cfg *Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current batch size: %d wallets\n", cfg.BatchSize)
	batchSize, ok := promptPositiveInt(reader, "  Enter new batch size (1-1000 wallets): ")
	if !ok {
		return
	}
	if err := validateBatchSize(batchSize); err != nil {
		fmt.Print(Error("\n[ERROR] %v\n", err))
		return
	}
	cfg.BatchSize = batchSize
	fmt.Print(Info("[INFO] Generation batch size set to %d wallets.\n", cfg.BatchSize))
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

func generateWallets(ctx context.Context, store Storage, cfg *Config, total int, reader *bufio.Reader) {
	// Show preview for the run
	previewGenerationRun(cfg, store, total)

	// Ask for confirmation if total exceeds threshold (10,000 wallets)
	if total > 10000 {
		if !confirmGeneration(total, reader) {
			fmt.Println(Info("\n[INFO] Generation cancelled by user."))
			return
		}
	}

	fmt.Print(Info("\n[INFO] Starting wallet generation\n"))
	fmt.Print(Info("[INFO] Generating %d wallets\n", total))

	start := time.Now()

	if err := GenerateWallets(ctx, store, cfg, total); err != nil {
		fmt.Print(Error("\n[ERROR] Generation failed: %v\n", err))
		return
	}

	elapsed := time.Since(start)

	// Show completion summary
	showCompletionSummary(total, elapsed, cfg)
}

func previewGenerationRun(cfg *Config, store Storage, total int) {
	batches := (total + cfg.BatchSize - 1) / cfg.BatchSize // Ceiling division
	loggingStatus := "enabled"
	if !cfg.EnableLogging {
		loggingStatus = "disabled"
	}

	storageLabel := store.StorageType()
	if cfg.StorageType == "postgres" {
		storageLabel = fmt.Sprintf("postgres (%s)", cfg.DBName)
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
  │  Storage        : %-33s │
  │  Logging        : %-33s │
  └──────────────────────────────────────────────────────┘
`,
		total,
		batches, cfg.BatchSize, "",
		cfg.Workers,
		cfg.BatchSize,
		batches,
		storageLabel,
		loggingStatus,
	)
}

func confirmGeneration(total int, reader *bufio.Reader) bool {
	fmt.Printf("\n  ⚠️  Large generation run: %d wallets\n", total)
	fmt.Print("  Continue? [y/N]: ")

	input := strings.ToLower(strings.TrimSpace(readLine(reader)))
	return input == "y" || input == "yes"
}

func showCompletionSummary(total int, elapsed time.Duration, cfg *Config) {
	walletsPerSec := float64(total) / elapsed.Seconds()

	// Skip detailed summary in minimal UI mode
	if isMinimalUI(cfg) {
		fmt.Print(Info("[INFO] ✓ Generated %s wallets in %s (~%.0f wallets/sec)\n\n",
			FormatNumber(total), elapsed.Round(time.Millisecond), walletsPerSec))
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
		FormatNumber(total),
		elapsed.Round(time.Millisecond),
		walletsPerSec,
		cfg.Workers,
		cfg.BatchSize,
		time.Duration(elapsed.Nanoseconds()/int64((total+cfg.BatchSize-1)/cfg.BatchSize)).Round(time.Millisecond),
		(total+cfg.BatchSize-1)/cfg.BatchSize,
	)
}

// ============================================================================
// Configuration Menu Helpers
// ============================================================================

func showCurrentSettings(cfg *Config) {
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

func changeWorkers(cfg *Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current workers: %d\n", cfg.Workers)
	fmt.Println("  Recommended: 8-32 (based on CPU cores)")
	fmt.Println("  Higher values increase throughput but use more CPU/memory")

	workers, ok := promptPositiveInt(reader, "  Enter new worker count (1-100): ")
	if !ok {
		return
	}

	if workers < 1 || workers > 100 {
		fmt.Println(Error("\n[ERROR] Worker count must be between 1 and 100."))
		fmt.Printf("        Current value: %d\n", cfg.Workers)
		fmt.Println("        Recommended: 8-32 for most systems")
		return
	}

	cfg.Workers = workers
	fmt.Print(Info("[INFO] Workers set to %d for this session.\n", cfg.Workers))
}

func toggleLogging(cfg *Config) {
	cfg.EnableLogging = !cfg.EnableLogging

	status := "enabled"
	if !cfg.EnableLogging {
		status = "disabled"
	}
	fmt.Print(Info("\n[INFO] Logging %s for this session.\n", status))
	if !cfg.EnableLogging {
		fmt.Println("       Note: Error and warning messages will still be shown.")
	}
}

func changePoolMonitorInterval(cfg *Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current pool monitor interval: %d seconds\n", cfg.PoolMonitorInterval)
	fmt.Println("  Set to 0 to disable pool monitoring")
	fmt.Println("  Recommended: 30-60 seconds for production")

	interval, ok := promptPositiveInt(reader, "  Enter new interval in seconds (0-300): ")
	if !ok {
		return
	}

	if interval < 0 || interval > 300 {
		fmt.Println(Error("\n[ERROR] Pool monitor interval must be between 0 and 300 seconds."))
		fmt.Printf("        Current value: %d seconds\n", cfg.PoolMonitorInterval)
		fmt.Println("        Set to 0 to disable, or 30-60 for normal monitoring")
		return
	}

	cfg.PoolMonitorInterval = interval
	if interval == 0 {
		fmt.Println(Info("[INFO] Pool monitoring disabled for this session."))
	} else {
		fmt.Print(Info("[INFO] Pool monitor interval set to %d seconds for this session.\n", cfg.PoolMonitorInterval))
	}
}

func changePoolWarningThreshold(cfg *Config, reader *bufio.Reader) {
	fmt.Printf("\n  Current pool warning threshold: %.2f\n", cfg.PoolWarningThreshold)
	fmt.Println("  This is the ratio of used/total connections that triggers a warning")
	fmt.Println("  Recommended: 0.7-0.9 (70%-90%)")

	fmt.Print("  Enter new threshold (0.1-1.0): ")
	input := strings.TrimSpace(readLine(reader))

	threshold, err := strconv.ParseFloat(input, 64)
	if err != nil {
		fmt.Println(Error("\n[ERROR] Please enter a valid decimal number (e.g., 0.8)"))
		return
	}

	if threshold <= 0 || threshold > 1.0 {
		fmt.Println(Error("\n[ERROR] Pool warning threshold must be between 0.1 and 1.0."))
		fmt.Printf("        Current value: %.2f\n", cfg.PoolWarningThreshold)
		fmt.Println("        Recommended: 0.7-0.9 (70%-90%)")
		return
	}

	cfg.PoolWarningThreshold = threshold
	fmt.Print(Info("[INFO] Pool warning threshold set to %.2f for this session.\n", cfg.PoolWarningThreshold))
}

func toggleUIMode(cfg *Config) {
	if cfg.UIMode == "full" {
		cfg.UIMode = "minimal"
		fmt.Print(Info("\n[INFO] UI mode set to 'minimal' for this session.\n"))
		fmt.Println("       Minimal mode shows less decorative elements and focuses on essential information.")
	} else {
		cfg.UIMode = "full"
		fmt.Print(Info("\n[INFO] UI mode set to 'full' for this session.\n"))
		fmt.Println("       Full mode shows all decorative elements and detailed information.")
	}
}

func resetSessionSettings(cfg *Config, originalCfg *Config, reader *bufio.Reader) {
	fmt.Print("\n  Reset all settings to .env defaults? [y/N]: ")
	input := strings.ToLower(strings.TrimSpace(readLine(reader)))

	if input != "y" && input != "yes" {
		fmt.Println(Info("[INFO] Reset cancelled."))
		return
	}

	// Restore original configuration
	cfg.Workers = originalCfg.Workers
	cfg.BatchSize = originalCfg.BatchSize
	cfg.EnableLogging = originalCfg.EnableLogging
	cfg.PoolMonitorInterval = originalCfg.PoolMonitorInterval
	cfg.PoolWarningThreshold = originalCfg.PoolWarningThreshold
	fmt.Println(Info("[INFO] All session settings reset to .env defaults."))
}

// ============================================================================
// Benchmark Menu Helpers
// ============================================================================

func estimateSettings(cfg *Config) {
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

		fmt.Printf("    %-20s : %s\n", s.name+" ("+FormatNumber(s.count)+")", timeStr)
	}
	fmt.Println()
}

func runSmallBenchmark(ctx context.Context, store Storage, cfg *Config) {
	fmt.Println(Info("\n[INFO] Running small benchmark (1000 wallets)..."))
	fmt.Println("       This will measure actual performance on your system.")
	fmt.Println("       Note: Wallets are NOT stored, this is for speed testing only.")

	elapsed, err := BenchmarkWalletGeneration(ctx, cfg, 1000)
	if err != nil {
		fmt.Print(Error("[ERROR] Benchmark failed: %v\n", err))
		return
	}

	walletsPerSec := 1000.0 / elapsed.Seconds()

	fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │            BENCHMARK RESULTS                         │
  ├──────────────────────────────────────────────────────┤
  │  Wallets generated : 1,000 (not stored)              │
  │  Time elapsed      : %-33s │
  │  Throughput        : ~%.0f wallets/second%-15s │
  │  Workers used      : %-33d │
  └──────────────────────────────────────────────────────┘
`, elapsed.Round(time.Millisecond), walletsPerSec, "", cfg.Workers)
}

func compareWorkerCounts(ctx context.Context, store Storage, cfg *Config) {
	fmt.Println(Info("\n[INFO] Comparing different worker counts..."))
	fmt.Println("       Testing with 500 wallets each (not stored)")

	originalWorkers := cfg.Workers

	workerCounts := []int{4, 8, 16, 32}
	results := make(map[int]time.Duration)

	for _, workers := range workerCounts {
		cfg.Workers = workers
		fmt.Printf("  Testing %d workers... ", workers)

		elapsed, err := BenchmarkWalletGeneration(ctx, cfg, 500)
		if err != nil {
			fmt.Printf("failed: %v\n", err)
			continue
		}
		results[workers] = elapsed

		walletsPerSec := 500.0 / elapsed.Seconds()
		fmt.Printf("%.2fs (~%.0f wallets/sec)\n", elapsed.Seconds(), walletsPerSec)
	}

	// Restore original settings
	cfg.Workers = originalWorkers

	fmt.Println("\n  Recommendation: Use the worker count with highest throughput")
	fmt.Println("                  that doesn't exceed your CPU capacity.")
}

func compareBatchSizes(ctx context.Context, store Storage, cfg *Config) {
	fmt.Println(Info("\n[INFO] Comparing different batch sizes..."))
	fmt.Println("       Testing with 1000 wallets each (not stored)")
	fmt.Println("       Note: Batch size affects storage performance, not generation speed.")

	// Batch size doesn't affect generation-only benchmarks
	// This test is kept for consistency but results will be similar
	batchSizes := []int{100, 250, 500, 1000}
	results := make(map[int]time.Duration)

	for _, batchSize := range batchSizes {
		fmt.Printf("  Testing batch size %d... ", batchSize)

		elapsed, err := BenchmarkWalletGeneration(ctx, cfg, 1000)
		if err != nil {
			fmt.Printf("failed: %v\n", err)
			continue
		}
		results[batchSize] = elapsed

		walletsPerSec := 1000.0 / elapsed.Seconds()
		fmt.Printf("%.2fs (~%.0f wallets/sec)\n", elapsed.Seconds(), walletsPerSec)
	}

	fmt.Println("\n  Note: Batch size primarily affects database insert performance,")
	fmt.Println("        not wallet generation speed. Use 500-1000 for best storage performance.")
}

// ============================================================================
// Help Menu Pages
// ============================================================================

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

// generatePreviewAddress creates a preview address showing the pattern
func generatePreviewAddress(prefix, suffix string) string {
	// Ethereum address is 40 hex characters (20 bytes)
	const addrLen = 40
	
	prefixLen := len(prefix)
	suffixLen := len(suffix)
	middleLen := addrLen - prefixLen - suffixLen
	
	if middleLen < 0 {
		middleLen = 0
	}
	
	// Generate middle part with 'x' characters
	middle := strings.Repeat("x", middleLen)
	
	return fmt.Sprintf("0x%s%s%s", prefix, middle, suffix)
}


// ============================================================================
// Vanity Pattern Parsing
// ============================================================================

// parseVanityPatterns parses user input into VanityPattern structs
// Supports formats: "prefix", "prefix...suffix", "p1,p2,p3"
func parseVanityPatterns(input string) []VanityPattern {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	// Split by comma for multiple patterns
	parts := strings.Split(input, ",")
	patterns := make([]VanityPattern, 0, len(parts))

	for i, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}

		// Check if pattern contains "..." separator
		if strings.Contains(part, "...") {
			split := strings.Split(part, "...")
			if len(split) == 2 {
				patterns = append(patterns, VanityPattern{
					Prefix: strings.TrimSpace(split[0]),
					Suffix: strings.TrimSpace(split[1]),
					Name:   fmt.Sprintf("Pattern %d", i+1),
				})
				continue
			}
		}

		// Single pattern (prefix only)
		patterns = append(patterns, VanityPattern{
			Prefix: part,
			Suffix: "",
			Name:   fmt.Sprintf("Pattern %d", i+1),
		})
	}

	return patterns
}

// isMinimalUI returns true if UI mode is set to minimal
func isMinimalUI(cfg *Config) bool {
	return cfg.UIMode == "minimal"
}