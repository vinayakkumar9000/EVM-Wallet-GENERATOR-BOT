// Package wallet provides exporter for plaintext wallet files.
package wallet

import (
	"bufio"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"evmwalletbot/config"
)

// Exporter handles plaintext export of wallet data to files.
// Supports multiple export modes: paired, key-only, address-only, combined (CSV).
type Exporter struct {
	config      config.Config
	addressFile *os.File
	keyFile     *os.File
	csvFile     *os.File
	csvWriter   *csv.Writer
	bufWriter   *bufio.Writer
	mu          sync.Mutex
	count       int
}

// NewExporter creates a new exporter with the given configuration.
// Creates output directory if it doesn't exist.
// Opens files based on export mode.
func NewExporter(cfg config.Config) (*Exporter, error) {
	if !cfg.ExportEnabled {
		return nil, fmt.Errorf("export is not enabled")
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(cfg.ExportDir, 0755); err != nil {
		return nil, fmt.Errorf("create export directory: %w", err)
	}

	e := &Exporter{
		config: cfg,
	}

	// Determine file open mode (append or truncate)
	openFlags := os.O_CREATE | os.O_WRONLY
	if cfg.ExportOverwrite {
		openFlags |= os.O_TRUNC
	} else {
		openFlags |= os.O_APPEND
	}

	var err error

	// Open files based on export mode
	switch cfg.ExportMode {
	case "paired":
		// Open both address and key files
		addressPath := filepath.Join(cfg.ExportDir, "address.txt")
		e.addressFile, err = os.OpenFile(addressPath, openFlags, 0644)
		if err != nil {
			return nil, fmt.Errorf("open address file: %w", err)
		}

		keyPath := filepath.Join(cfg.ExportDir, "privatekey.txt")
		e.keyFile, err = os.OpenFile(keyPath, openFlags, 0644)
		if err != nil {
			e.addressFile.Close()
			return nil, fmt.Errorf("open key file: %w", err)
		}

	case "key-only":
		keyPath := filepath.Join(cfg.ExportDir, "privatekey.txt")
		e.keyFile, err = os.OpenFile(keyPath, openFlags, 0644)
		if err != nil {
			return nil, fmt.Errorf("open key file: %w", err)
		}

	case "address-only":
		addressPath := filepath.Join(cfg.ExportDir, "address.txt")
		e.addressFile, err = os.OpenFile(addressPath, openFlags, 0644)
		if err != nil {
			return nil, fmt.Errorf("open address file: %w", err)
		}

	case "combined":
		csvPath := filepath.Join(cfg.ExportDir, "wallets.csv")
		e.csvFile, err = os.OpenFile(csvPath, openFlags, 0644)
		if err != nil {
			return nil, fmt.Errorf("open CSV file: %w", err)
		}
		e.csvWriter = csv.NewWriter(e.csvFile)

		// Write CSV header if file is new or overwrite mode
		if cfg.ExportOverwrite {
			if err := e.csvWriter.Write([]string{"address", "private_key"}); err != nil {
				e.csvFile.Close()
				return nil, fmt.Errorf("write CSV header: %w", err)
			}
		}

	default:
		return nil, fmt.Errorf("invalid export mode: %s", cfg.ExportMode)
	}

	return e, nil
}

// Export writes a single wallet to the export files.
// Thread-safe: can be called concurrently from multiple goroutines.
func (e *Exporter) Export(address, privateKey string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Format address with optional checksum and prefix
	formattedAddress := address
	if e.config.ExportUseChecksum {
		// Convert to EIP-55 checksum format
		formattedAddress = common.HexToAddress(address).Hex()
	}
	if !e.config.ExportAddressPrefix && len(formattedAddress) > 2 && formattedAddress[:2] == "0x" {
		formattedAddress = formattedAddress[2:]
	}

	// Format private key with optional prefix
	formattedKey := privateKey
	if !e.config.ExportKeyPrefix && len(formattedKey) > 2 && formattedKey[:2] == "0x" {
		formattedKey = formattedKey[2:]
	}

	// Write based on export mode
	switch e.config.ExportMode {
	case "paired":
		// Write to both files, maintaining line-for-line sync
		if _, err := fmt.Fprintln(e.addressFile, formattedAddress); err != nil {
			return fmt.Errorf("write address: %w", err)
		}
		if _, err := fmt.Fprintln(e.keyFile, formattedKey); err != nil {
			return fmt.Errorf("write key: %w", err)
		}

	case "key-only":
		if _, err := fmt.Fprintln(e.keyFile, formattedKey); err != nil {
			return fmt.Errorf("write key: %w", err)
		}

	case "address-only":
		if _, err := fmt.Fprintln(e.addressFile, formattedAddress); err != nil {
			return fmt.Errorf("write address: %w", err)
		}

	case "combined":
		if err := e.csvWriter.Write([]string{formattedAddress, formattedKey}); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}

	e.count++

	// Flush every 1000 wallets to ensure data is written
	if e.count%1000 == 0 {
		if err := e.Flush(); err != nil {
			return fmt.Errorf("flush after %d wallets: %w", e.count, err)
		}
	}

	return nil
}

// Flush ensures all buffered data is written to disk.
func (e *Exporter) Flush() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.csvWriter != nil {
		e.csvWriter.Flush()
		if err := e.csvWriter.Error(); err != nil {
			return fmt.Errorf("flush CSV writer: %w", err)
		}
	}

	if e.addressFile != nil {
		if err := e.addressFile.Sync(); err != nil {
			return fmt.Errorf("sync address file: %w", err)
		}
	}

	if e.keyFile != nil {
		if err := e.keyFile.Sync(); err != nil {
			return fmt.Errorf("sync key file: %w", err)
		}
	}

	if e.csvFile != nil {
		if err := e.csvFile.Sync(); err != nil {
			return fmt.Errorf("sync CSV file: %w", err)
		}
	}

	return nil
}

// Close flushes and closes all open files.
func (e *Exporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error

	// Flush CSV writer before closing
	if e.csvWriter != nil {
		e.csvWriter.Flush()
		if err := e.csvWriter.Error(); err != nil {
			errs = append(errs, fmt.Errorf("flush CSV writer: %w", err))
		}
	}

	// Close all files
	if e.addressFile != nil {
		if err := e.addressFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close address file: %w", err))
		}
	}

	if e.keyFile != nil {
		if err := e.keyFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close key file: %w", err))
		}
	}

	if e.csvFile != nil {
		if err := e.csvFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close CSV file: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close exporter: %v", errs)
	}

	return nil
}

// Count returns the number of wallets exported.
func (e *Exporter) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

// VerifyExportedWallet verifies that an exported address matches its private key.
// Returns true if the address can be derived from the private key.
func VerifyExportedWallet(address, privateKey string) (bool, error) {
	// Remove 0x prefix if present
	if len(privateKey) > 2 && privateKey[:2] == "0x" {
		privateKey = privateKey[2:]
	}
	if len(address) > 2 && address[:2] == "0x" {
		address = address[2:]
	}

	// Decode private key
	keyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return false, fmt.Errorf("decode private key: %w", err)
	}

	// Create ECDSA private key
	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return false, fmt.Errorf("create ECDSA key: %w", err)
	}

	// Derive address from private key
	derivedAddress := crypto.PubkeyToAddress(privKey.PublicKey)

	// Compare addresses (case-insensitive)
	expectedAddress := common.HexToAddress(address)

	return derivedAddress == expectedAddress, nil
}
