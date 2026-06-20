package internal

import (
	"bufio"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// VerifyExportedFile re-derives addresses from an exported key file and reports pass/fail.
// This is an optional, off-by-default verification tool that runs outside normal generation.
func VerifyExportedFile(filePath string) error {
	fmt.Printf("\n%s\n", Highlight("VERIFY EXPORTED WALLETS"))
	fmt.Printf("  File: %s\n\n", filePath)

	// Detect file format
	ext := strings.ToLower(filePath[len(filePath)-4:])

	var err error
	switch ext {
	case ".txt":
		err = verifyTxtFile(filePath)
	case ".csv":
		err = verifyCSVFile(filePath)
	case "json":
		err = verifyJSONFile(filePath)
	default:
		return fmt.Errorf("unsupported file format: %s (supported: .txt, .csv, .json)", ext)
	}

	return err
}

// verifyTxtFile verifies a paired txt export (addresses.txt + keys.txt)
func verifyTxtFile(filePath string) error {
	// Determine if this is addresses.txt or keys.txt
	var addressFile, keyFile string

	if strings.HasSuffix(filePath, "addresses.txt") {
		addressFile = filePath
		keyFile = strings.Replace(filePath, "addresses.txt", "keys.txt", 1)
	} else if strings.HasSuffix(filePath, "keys.txt") {
		keyFile = filePath
		addressFile = strings.Replace(filePath, "keys.txt", "addresses.txt", 1)
	} else {
		return fmt.Errorf("txt file must be named 'addresses.txt' or 'keys.txt'")
	}

	// Read addresses
	addresses, err := readLines(addressFile)
	if err != nil {
		return fmt.Errorf("read addresses: %w", err)
	}

	// Read keys
	keys, err := readLines(keyFile)
	if err != nil {
		return fmt.Errorf("read keys: %w", err)
	}

	if len(addresses) != len(keys) {
		return fmt.Errorf("mismatch: %d addresses but %d keys", len(addresses), len(keys))
	}

	return verifyPairs(addresses, keys)
}

// verifyCSVFile verifies a CSV export
func verifyCSVFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	if len(records) == 0 {
		return fmt.Errorf("empty CSV file")
	}

	// Skip header if present
	start := 0
	if len(records[0]) == 2 && (records[0][0] == "address" || records[0][0] == "Address") {
		start = 1
	}

	var addresses, keys []string
	for i := start; i < len(records); i++ {
		if len(records[i]) != 2 {
			return fmt.Errorf("invalid CSV row %d: expected 2 columns, got %d", i+1, len(records[i]))
		}
		addresses = append(addresses, records[i][0])
		keys = append(keys, records[i][1])
	}

	return verifyPairs(addresses, keys)
}

// verifyJSONFile verifies a JSON export
func verifyJSONFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var wallets []struct {
		Address    string `json:"address"`
		PrivateKey string `json:"private_key"`
	}

	if err := json.Unmarshal(data, &wallets); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	var addresses, keys []string
	for _, w := range wallets {
		addresses = append(addresses, w.Address)
		keys = append(keys, w.PrivateKey)
	}

	return verifyPairs(addresses, keys)
}

// verifyPairs verifies that each private key derives to its paired address
func verifyPairs(addresses, keys []string) error {
	total := len(addresses)
	passed := 0
	failed := 0

	fmt.Printf("  Verifying %d wallet pairs...\n\n", total)

	for i := 0; i < total; i++ {
		addr := strings.TrimSpace(addresses[i])
		key := strings.TrimSpace(keys[i])

		// Normalize: remove 0x prefix
		if strings.HasPrefix(addr, "0x") || strings.HasPrefix(addr, "0X") {
			addr = addr[2:]
		}
		if strings.HasPrefix(key, "0x") || strings.HasPrefix(key, "0X") {
			key = key[2:]
		}

		// Decode private key
		keyBytes, err := hex.DecodeString(key)
		if err != nil {
			fmt.Printf("  %s Wallet %d: Invalid private key hex\n", Error("✗"), i+1)
			failed++
			continue
		}

		// Derive address
		privKey, err := crypto.ToECDSA(keyBytes)
		if err != nil {
			fmt.Printf("  %s Wallet %d: Invalid private key\n", Error("✗"), i+1)
			failed++
			continue
		}

		derivedAddr := crypto.PubkeyToAddress(privKey.PublicKey)
		derivedHex := strings.ToLower(hex.EncodeToString(derivedAddr.Bytes()))
		expectedHex := strings.ToLower(addr)

		if derivedHex != expectedHex {
			fmt.Printf("  %s Wallet %d: Address mismatch\n", Error("✗"), i+1)
			fmt.Printf("      Expected: %s\n", expectedHex)
			fmt.Printf("      Derived:  %s\n", derivedHex)
			failed++
		} else {
			passed++
		}
	}

	// Summary
	fmt.Printf("\n%s\n", Highlight("VERIFICATION SUMMARY"))
	fmt.Printf("  Total:  %d\n", total)
	fmt.Printf("  %s Passed: %d\n", Success("✓"), passed)

	if failed > 0 {
		fmt.Printf("  %s Failed: %d\n\n", Error("✗"), failed)
		return fmt.Errorf("verification failed: %d/%d wallets have mismatched addresses", failed, total)
	}

	fmt.Printf("\n  %s All wallets verified successfully!\n\n", Success("✓"))
	return nil
}

// readLines reads all lines from a file
func readLines(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}
