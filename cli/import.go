// Package cli — import and verify private key handlers
package cli

import (
	"bufio"
	"fmt"
	"strings"

	"evmwalletbot/core"
	"evmwalletbot/wallet"
)

// handleImportVerify handles the import and verify private key menu
func handleImportVerify(reader *bufio.Reader) {
	core.ClearScreenIfEnabled()

	fmt.Printf("\n%s\n", core.Highlight("IMPORT & VERIFY PRIVATE KEY"))
	fmt.Println("  Paste a private key to verify and see its address.")
	fmt.Println("  The key is NOT saved to the database.")
	fmt.Println()

	// Prompt for private key
	fmt.Print("  Private key (with or without 0x prefix): ")
	privateKeyInput := strings.TrimSpace(readLine(reader))

	if privateKeyInput == "" {
		fmt.Println(core.Warning("\n[WARN] No private key provided."))
		fmt.Print("\n  Press Enter to continue...")
		readLine(reader)
		return
	}

	// Import and verify the private key
	result := wallet.ImportPrivateKey(privateKeyInput)

	// Clear screen and display result
	core.ClearScreenIfEnabled()
	fmt.Printf("\n%s\n", core.Highlight("IMPORT & VERIFY RESULT"))
	fmt.Println()

	if !result.Valid {
		// Display error
		fmt.Printf("  %s Invalid private key\n", core.Error("✗"))
		fmt.Printf("  %s %s\n\n", core.Error("Error:"), result.Error)
	} else {
		// Display success
		fmt.Printf("  %s Valid private key\n\n", core.Success("✓"))

		// Display normalized private key
		fmt.Printf("  %s\n", core.Highlight("Private Key (normalized):"))
		fmt.Printf("  %s\n\n", result.PrivateKey)

		// Display derived address (EIP-55 checksum)
		fmt.Printf("  %s\n", core.Highlight("Derived Address (EIP-55):"))
		fmt.Printf("  %s\n\n", result.Address)

		// Show verification info
		fmt.Printf("  %s This address was derived from the provided private key.\n", core.Info("ℹ"))
		fmt.Printf("  %s The key was NOT saved to the database.\n", core.Info("ℹ"))
		fmt.Println()
	}

	fmt.Print("  Press Enter to continue...")
	readLine(reader)
}
