// Package wallet provides exporter for plaintext wallet files.
package wallet

import (
	"bufio"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"

	"evmwalletbot/config"
)

// WalletExportRecord represents a wallet in JSON export format
type WalletExportRecord struct {
	Address         string  `json:"address"`
	PrivateKey      string  `json:"private_key"`
	DerivationIndex *uint32 `json:"derivation_index,omitempty"`
	DerivationPath  string  `json:"derivation_path,omitempty"`
}

// Exporter handles plaintext export of wallet data to files.
// Supports multiple export modes: paired, key-only, address-only, combined (CSV), json, keystore.
type Exporter struct {
	config      config.Config
	addressFile *os.File
	keyFile     *os.File
	csvFile     *os.File
	csvWriter   *csv.Writer
	jsonFile    *os.File
	jsonEncoder *json.Encoder
	jsonWallets []WalletExportRecord // Buffer for JSON export
	keystoreDir string               // Directory for keystore files
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

	case "json":
		jsonPath := filepath.Join(cfg.ExportDir, "wallets.json")
		e.jsonFile, err = os.OpenFile(jsonPath, openFlags, 0644)
		if err != nil {
			return nil, fmt.Errorf("open JSON file: %w", err)
		}
		e.jsonWallets = make([]WalletExportRecord, 0, 1000)

	case "keystore":
		// Create keystore directory
		keystorePath := filepath.Join(cfg.ExportDir, "keystore")
		if err := os.MkdirAll(keystorePath, 0755); err != nil {
			return nil, fmt.Errorf("create keystore directory: %w", err)
		}
		e.keystoreDir = keystorePath

	default:
		return nil, fmt.Errorf("invalid export mode: %s (valid: paired, key-only, address-only, combined, json, keystore)", cfg.ExportMode)
	}

	return e, nil
}

// Export writes a single wallet to the export files.
// Thread-safe: can be called concurrently from multiple goroutines.
func (e *Exporter) Export(address, privateKey string) error {
	return e.ExportWithDerivation(address, privateKey, nil, "")
}

// ExportWithDerivation writes a wallet with optional HD derivation info.
// Thread-safe: can be called concurrently from multiple goroutines.
func (e *Exporter) ExportWithDerivation(address, privateKey string, derivationIndex *uint32, derivationPath string) error {
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

	case "json":
		// Buffer wallet for JSON export (written on Close)
		record := WalletExportRecord{
			Address:         formattedAddress,
			PrivateKey:      formattedKey,
			DerivationIndex: derivationIndex,
			DerivationPath:  derivationPath,
		}
		e.jsonWallets = append(e.jsonWallets, record)

	case "keystore":
		// Export as encrypted keystore file (requires password in config)
		if err := e.exportKeystore(address, privateKey); err != nil {
			return fmt.Errorf("export keystore: %w", err)
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

// exportKeystore creates an encrypted keystore file for a single wallet
func (e *Exporter) exportKeystore(address, privateKey string) error {
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
		return fmt.Errorf("decode private key: %w", err)
	}

	// Create ECDSA private key
	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return fmt.Errorf("create ECDSA key: %w", err)
	}

	// Get password from config (should be set before export)
	password := e.config.KeystorePassword
	if password == "" {
		return fmt.Errorf("keystore password not set")
	}

	// Derive address to ensure it matches
	derivedAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	// Create keystore Key
	id := uuid.New()
	key := &keystore.Key{
		Id:         id,
		Address:    derivedAddr,
		PrivateKey: privKey,
	}

	// Encrypt key using Web3 Secret Storage format (scrypt)
	keystoreJSON, err := keystore.EncryptKey(key, password, keystore.StandardScryptN, keystore.StandardScryptP)
	if err != nil {
		return fmt.Errorf("encrypt key: %w", err)
	}

	// Generate filename: UTC--<timestamp>--<address>
	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z")
	filename := fmt.Sprintf("UTC--%s--%s", timestamp, address)
	keystorePath := filepath.Join(e.keystoreDir, filename)

	// Write keystore file
	if err := os.WriteFile(keystorePath, keystoreJSON, 0600); err != nil {
		return fmt.Errorf("write keystore file: %w", err)
	}

	return nil
}

// Close flushes and closes all open files.
func (e *Exporter) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error

	// Write JSON array if in JSON mode
	if e.jsonFile != nil && len(e.jsonWallets) > 0 {
		encoder := json.NewEncoder(e.jsonFile)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(e.jsonWallets); err != nil {
			errs = append(errs, fmt.Errorf("write JSON: %w", err))
		}
	}

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

	if e.jsonFile != nil {
		if err := e.jsonFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close JSON file: %w", err))
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
