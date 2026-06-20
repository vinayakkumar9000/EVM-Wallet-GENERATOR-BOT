// Package cli — vanity menu handlers
package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"evmwalletbot/config"
	"evmwalletbot/core"
	"evmwalletbot/storage"
	"evmwalletbot/wallet"
)

// handleVanityMenuNew handles the vanity generation menu with multi-pattern support
func handleVanityMenuNew(ctx context.Context, store storage.Storage, cfg *config.Config, reader *bufio.Reader) {
	core.ClearScreenIfEnabled()

	fmt.Printf("\n%s\n", core.Highlight("VANITY ADDRESS GENERATION"))
	fmt.Println("  Generate wallets with custom prefix and/or suffix patterns.")
	fmt.Println("  Tip: Use comma-separated patterns for multiple (e.g., 'abc,def,123')")
	fmt.Println()

	// Prompt for patterns
	fmt.Print("  Pattern(s) - format: prefix...suffix or comma-separated\n")
	fmt.Print("  Examples: 'dead' or 'cafe...beef' or 'abc,def,123'\n")
	fmt.Print("  > ")
	patternInput := strings.TrimSpace(readLine(reader))

	if patternInput == "" {
		fmt.Println(core.Warning("\n[WARN] No pattern specified."))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	// Parse patterns
	patterns := parseVanityPatterns(patternInput)
	if len(patterns) == 0 {
		fmt.Println(core.Error("\n[ERROR] No valid patterns provided"))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	// Validate all patterns
	for i, p := range patterns {
		if err := wallet.ValidateVanityPattern(p.Prefix, fmt.Sprintf("pattern %d prefix", i+1)); err != nil {
			fmt.Print(core.Error("\n[ERROR] %v\n", err))
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
			return
		}
		if err := wallet.ValidateVanityPattern(p.Suffix, fmt.Sprintf("pattern %d suffix", i+1)); err != nil {
			fmt.Print(core.Error("\n[ERROR] %v\n", err))
			fmt.Print("\n  Press Enter to continue...")
			readLine(reader)
			return
		}
	}

	// Prompt for checksum mode
	fmt.Print("\n  Case-sensitive matching (EIP-55 checksum, harder)? [y/N]: ")
	checksumInput := strings.ToLower(strings.TrimSpace(readLine(reader)))
	checksum := checksumInput == "y" || checksumInput == "yes"

	// Show difficulty update if checksum enabled
	if checksum {
		difficulty := wallet.CalculateMultiPatternDifficulty(patterns, checksum)
		fmt.Printf("  %s Checksum mode increases difficulty to %s\n",
			core.Info("ℹ"), wallet.FormatDifficulty(difficulty))
	}

	// Prompt for wallet count
	var count int
	for {
		countVal, ok := promptPositiveInt(reader, "\n  Number of matching wallets to generate: ")
		if !ok {
			continue
		}
		count = countVal
		break
	}

	// Create vanity config
	vanityConfig := core.VanityConfig{
		Patterns:    patterns,
		Checksum:    checksum,
		TargetCount: count,
	}

	// Generate vanity wallets
	if err := core.GenerateVanityWallets(ctx, store, cfg, vanityConfig); err != nil {
		fmt.Print(core.Error("\n[ERROR] Vanity generation failed: %v\n", err))
	}

	fmt.Print("\n  Press Enter to continue...")
	readLine(reader)
}

// parseVanityPatterns parses user input into VanityPattern structs
// Supports formats: "prefix", "prefix...suffix", "p1,p2,p3"
func parseVanityPatterns(input string) []wallet.VanityPattern {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	// Split by comma for multiple patterns
	parts := strings.Split(input, ",")
	patterns := make([]wallet.VanityPattern, 0, len(parts))

	for i, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}

		// Check if pattern contains "..." separator
		if strings.Contains(part, "...") {
			split := strings.Split(part, "...")
			if len(split) == 2 {
				patterns = append(patterns, wallet.VanityPattern{
					Prefix: strings.TrimSpace(split[0]),
					Suffix: strings.TrimSpace(split[1]),
					Name:   fmt.Sprintf("Pattern %d", i+1),
				})
				continue
			}
		}

		// Single pattern (prefix only)
		patterns = append(patterns, wallet.VanityPattern{
			Prefix: part,
			Suffix: "",
			Name:   fmt.Sprintf("Pattern %d", i+1),
		})
	}

	return patterns
}
