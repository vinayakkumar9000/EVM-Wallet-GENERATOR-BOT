// Package cli — HD wallet generation menu handlers
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"evmwalletbot/config"
	"evmwalletbot/core"
	"evmwalletbot/storage"
	"evmwalletbot/wallet"
)

// generateFromSeedPhrase handles the HD wallet generation menu flow
func generateFromSeedPhrase(ctx context.Context, store storage.Storage, cfg *config.Config) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println(core.Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println(core.Highlight("  HD WALLET GENERATION (BIP-39/BIP-44)"))
	fmt.Println(core.Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println()

	// Step 1: Choose to generate new or use existing mnemonic
	fmt.Println("Options:")
	fmt.Println("  1. Generate new seed phrase (12 words)")
	fmt.Println("  2. Generate new seed phrase (24 words)")
	fmt.Println("  3. Use existing seed phrase")
	fmt.Println("  0. Back to main menu")
	fmt.Println()
	fmt.Print("Choose option: ")

	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	var mnemonic string
	var err error

	switch choice {
	case "1":
		// Generate 12-word mnemonic
		mnemonic, err = wallet.GenerateMnemonic(wallet.Mnemonic12Words)
		if err != nil {
			fmt.Println(core.Error("[ERROR] Failed to generate mnemonic: %v", err))
			return
		}
		displayMnemonic(mnemonic, 12)

	case "2":
		// Generate 24-word mnemonic
		mnemonic, err = wallet.GenerateMnemonic(wallet.Mnemonic24Words)
		if err != nil {
			fmt.Println(core.Error("[ERROR] Failed to generate mnemonic: %v", err))
			return
		}
		displayMnemonic(mnemonic, 24)

	case "3":
		// Use existing mnemonic
		fmt.Println()
		fmt.Println("Enter your BIP-39 seed phrase (12 or 24 words):")
		fmt.Print("> ")
		mnemonic, _ = reader.ReadString('\n')
		mnemonic = strings.TrimSpace(mnemonic)

		// Validate mnemonic
		if err := wallet.ValidateMnemonic(mnemonic); err != nil {
			fmt.Println(core.Error("[ERROR] Invalid seed phrase: %v", err))
			fmt.Println("       Please check your seed phrase and try again.")
			return
		}
		fmt.Println(core.Success("[✓] Seed phrase validated successfully"))

	case "0":
		return

	default:
		fmt.Println(core.Error("[ERROR] Invalid option"))
		return
	}

	// Step 2: Optional passphrase
	fmt.Println()
	fmt.Println("Optional BIP-39 passphrase (press Enter to skip):")
	fmt.Print("> ")
	passphrase, _ := reader.ReadString('\n')
	passphrase = strings.TrimSpace(passphrase)

	if passphrase != "" {
		fmt.Println(core.Info("[INFO] Using passphrase (this will generate different addresses)"))
	}

	// Step 3: Number of addresses to derive
	fmt.Println()
	fmt.Print("How many addresses to derive? (1-100): ")
	countStr, _ := reader.ReadString('\n')
	countStr = strings.TrimSpace(countStr)

	count, err := strconv.Atoi(countStr)
	if err != nil || count < 1 || count > 100 {
		fmt.Println(core.Error("[ERROR] Invalid count. Must be between 1 and 100"))
		return
	}

	// Step 4: Derive wallets
	fmt.Println()
	fmt.Println(core.Info("[INFO] Deriving %d addresses from seed phrase...", count))

	hdConfig := wallet.HDWalletConfig{
		Mnemonic:   mnemonic,
		Passphrase: passphrase,
		StartIndex: 0,
		Count:      uint32(count),
	}

	wallets, err := wallet.DeriveWalletsFromMnemonic(hdConfig)
	if err != nil {
		fmt.Println(core.Error("[ERROR] Failed to derive wallets: %v", err))
		return
	}

	// Step 5: Display derived wallets
	fmt.Println()
	fmt.Println(core.Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println(core.Highlight("  DERIVED WALLETS (BIP-44: m/44'/60'/0'/0/i)"))
	fmt.Println(core.Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println()

	for i, w := range wallets {
		fmt.Printf("  [%d] %s\n", i, core.Highlight("m/44'/60'/0'/0/%d", i))
		fmt.Printf("      Address:     0x%s\n", w.AddressHex())
		fmt.Printf("      Private Key: 0x%s\n", w.PrivateKeyHex())
		fmt.Println()
	}

	// Step 6: Save to database
	fmt.Print("Save these wallets to database? (y/n): ")
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))

	if confirm != "y" && confirm != "yes" {
		fmt.Println(core.Info("[INFO] Wallets not saved"))
		return
	}

	// Save wallets
	ids, err := store.SaveWallets(ctx, wallets)
	if err != nil {
		fmt.Println(core.Error("[ERROR] Failed to save wallets: %v", err))
		return
	}

	fmt.Println()
	fmt.Println(core.Success("[✓] Successfully saved %d HD wallets to database", len(ids)))

	// Step 7: Export option
	if cfg.ExportEnabled {
		fmt.Println()
		fmt.Print("Export wallets to file? (y/n): ")
		exportConfirm, _ := reader.ReadString('\n')
		exportConfirm = strings.TrimSpace(strings.ToLower(exportConfirm))

		if exportConfirm == "y" || exportConfirm == "yes" {
			exporter, err := wallet.NewExporter(*cfg)
			if err != nil {
				fmt.Println(core.Error("[ERROR] Failed to initialize exporter: %v", err))
				return
			}
			defer exporter.Close()

			for _, w := range wallets {
				if err := exporter.Export(w.AddressHex(), "0x"+w.PrivateKeyHex()); err != nil {
					fmt.Println(core.Error("[WARN] Export failed for wallet: %v", err))
				}
			}
			fmt.Println(core.Success("[✓] Wallets exported to %s", cfg.ExportDir))
		}
	}
}

// displayMnemonic shows the generated mnemonic with a strong warning
func displayMnemonic(mnemonic string, wordCount int) {
	fmt.Println()
	fmt.Println(core.Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println(core.Highlight("  YOUR %d-WORD SEED PHRASE", wordCount))
	fmt.Println(core.Highlight("═══════════════════════════════════════════════════════"))
	fmt.Println()
	fmt.Println(core.Highlight("  %s", mnemonic))
	fmt.Println()
	fmt.Println(core.Error("  ⚠️  CRITICAL SECURITY WARNING ⚠️"))
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
