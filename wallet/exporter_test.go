package wallet

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"evmwalletbot/config"
)

// TestVerifyExportedWallet verifies that address derivation from private key works correctly
func TestVerifyExportedWallet(t *testing.T) {
	// Generate a valid wallet for testing
	validWallet, err := Generate()
	if err != nil {
		t.Fatalf("Failed to generate test wallet: %v", err)
	}
	validAddress := validWallet.AddressHex()
	validKey := "0x" + validWallet.PrivateKeyHex()

	tests := []struct {
		name       string
		address    string
		privateKey string
		wantValid  bool
		wantError  bool
	}{
		{
			name:       "valid wallet with 0x prefix",
			address:    validAddress,
			privateKey: validKey,
			wantValid:  true,
			wantError:  false,
		},
		{
			name:       "valid wallet without 0x prefix",
			address:    validAddress[2:],
			privateKey: validKey[2:],
			wantValid:  true,
			wantError:  false,
		},
		{
			name:       "mismatched address and key",
			address:    "0x0000000000000000000000000000000000000000",
			privateKey: "0x8f2a55949038a9610f50fb23b5883af3b4ecb3c3bb792cbcefbd1542c692be63",
			wantValid:  false,
			wantError:  false,
		},
		{
			name:       "invalid private key hex",
			address:    "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
			privateKey: "invalid_hex",
			wantValid:  false,
			wantError:  true,
		},
		{
			name:       "private key too short",
			address:    "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
			privateKey: "0x1234",
			wantValid:  false,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := VerifyExportedWallet(tt.address, tt.privateKey)

			if tt.wantError {
				if err == nil {
					t.Errorf("VerifyExportedWallet() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("VerifyExportedWallet() unexpected error: %v", err)
				return
			}

			if valid != tt.wantValid {
				t.Errorf("VerifyExportedWallet() = %v, want %v", valid, tt.wantValid)
			}
		})
	}
}

// TestExporterPairedMode tests paired export mode (address.txt + privatekey.txt)
func TestExporterPairedMode(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.Config{
		ExportEnabled:       true,
		ExportMode:          "paired",
		ExportDir:           tmpDir,
		ExportOverwrite:     true,
		ExportAddressPrefix: true,
		ExportKeyPrefix:     true,
		ExportUseChecksum:   true,
	}

	exporter, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter() failed: %v", err)
	}
	defer exporter.Close()

	// Export test wallets - generate fresh ones to ensure valid address/key pairs
	testWallets := make([]struct {
		address string
		key     string
	}, 2)

	for i := 0; i < 2; i++ {
		w, err := Generate()
		if err != nil {
			t.Fatalf("Generate() failed: %v", err)
		}
		testWallets[i].address = w.AddressHex()
		testWallets[i].key = "0x" + w.PrivateKeyHex()
	}

	for _, w := range testWallets {
		if err := exporter.Export(w.address, w.key); err != nil {
			t.Fatalf("Export() failed: %v", err)
		}
	}

	if err := exporter.Flush(); err != nil {
		t.Fatalf("Flush() failed: %v", err)
	}

	if err := exporter.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify address file
	addressData, err := os.ReadFile(filepath.Join(tmpDir, "address.txt"))
	if err != nil {
		t.Fatalf("Failed to read address file: %v", err)
	}
	addressLines := strings.Split(strings.TrimSpace(string(addressData)), "\n")
	if len(addressLines) != len(testWallets) {
		t.Errorf("Expected %d addresses, got %d", len(testWallets), len(addressLines))
	}

	// Verify key file
	keyData, err := os.ReadFile(filepath.Join(tmpDir, "privatekey.txt"))
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}
	keyLines := strings.Split(strings.TrimSpace(string(keyData)), "\n")
	if len(keyLines) != len(testWallets) {
		t.Errorf("Expected %d keys, got %d", len(testWallets), len(keyLines))
	}

	// Verify line-for-line correspondence
	for i := range testWallets {
		if i >= len(addressLines) || i >= len(keyLines) {
			break
		}

		// Verify exported wallet is valid
		valid, err := VerifyExportedWallet(addressLines[i], keyLines[i])
		if err != nil {
			t.Errorf("Wallet %d verification failed: %v", i, err)
		}
		if !valid {
			t.Errorf("Wallet %d: address does not match private key", i)
		}
	}
}

// TestExporterKeyOnlyMode tests key-only export mode
func TestExporterKeyOnlyMode(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.Config{
		ExportEnabled:   true,
		ExportMode:      "key-only",
		ExportDir:       tmpDir,
		ExportOverwrite: true,
		ExportKeyPrefix: false, // Test without prefix
	}

	exporter, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter() failed: %v", err)
	}
	defer exporter.Close()

	testKey := "8f2a55949038a9610f50fb23b5883af3b4ecb3c3bb792cbcefbd1542c692be63"
	if err := exporter.Export("0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb", "0x"+testKey); err != nil {
		t.Fatalf("Export() failed: %v", err)
	}

	if err := exporter.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify only key file exists
	keyData, err := os.ReadFile(filepath.Join(tmpDir, "privatekey.txt"))
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}

	keyLine := strings.TrimSpace(string(keyData))
	if keyLine != testKey {
		t.Errorf("Expected key %s, got %s", testKey, keyLine)
	}

	// Verify address file does not exist
	if _, err := os.Stat(filepath.Join(tmpDir, "address.txt")); !os.IsNotExist(err) {
		t.Error("address.txt should not exist in key-only mode")
	}
}

// TestExporterAddressOnlyMode tests address-only export mode
func TestExporterAddressOnlyMode(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.Config{
		ExportEnabled:       true,
		ExportMode:          "address-only",
		ExportDir:           tmpDir,
		ExportOverwrite:     true,
		ExportAddressPrefix: false, // Test without prefix
		ExportUseChecksum:   false, // Test without checksum
	}

	exporter, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter() failed: %v", err)
	}
	defer exporter.Close()

	testAddress := "742d35Cc6634C0532925a3b844Bc9e7595f0bEb"
	if err := exporter.Export("0x"+testAddress, "0x8f2a55949038a9610f50fb23b5883af3b4ecb3c3bb792cbcefbd1542c692be63"); err != nil {
		t.Fatalf("Export() failed: %v", err)
	}

	if err := exporter.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify only address file exists
	addressData, err := os.ReadFile(filepath.Join(tmpDir, "address.txt"))
	if err != nil {
		t.Fatalf("Failed to read address file: %v", err)
	}

	addressLine := strings.TrimSpace(string(addressData))
	if !strings.EqualFold(addressLine, testAddress) {
		t.Errorf("Expected address %s, got %s", testAddress, addressLine)
	}

	// Verify key file does not exist
	if _, err := os.Stat(filepath.Join(tmpDir, "privatekey.txt")); !os.IsNotExist(err) {
		t.Error("privatekey.txt should not exist in address-only mode")
	}
}

// TestExporterCombinedMode tests combined CSV export mode
func TestExporterCombinedMode(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.Config{
		ExportEnabled:       true,
		ExportMode:          "combined",
		ExportDir:           tmpDir,
		ExportOverwrite:     true,
		ExportAddressPrefix: true,
		ExportKeyPrefix:     true,
		ExportUseChecksum:   true,
	}

	exporter, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter() failed: %v", err)
	}
	defer exporter.Close()

	// Generate valid test wallets
	testWallets := make([]struct {
		address string
		key     string
	}, 2)

	for i := 0; i < 2; i++ {
		w, err := Generate()
		if err != nil {
			t.Fatalf("Generate() failed: %v", err)
		}
		testWallets[i].address = w.AddressHex()
		testWallets[i].key = "0x" + w.PrivateKeyHex()
	}

	for _, w := range testWallets {
		if err := exporter.Export(w.address, w.key); err != nil {
			t.Fatalf("Export() failed: %v", err)
		}
	}

	if err := exporter.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify CSV file
	csvFile, err := os.Open(filepath.Join(tmpDir, "wallets.csv"))
	if err != nil {
		t.Fatalf("Failed to open CSV file: %v", err)
	}
	defer csvFile.Close()

	reader := csv.NewReader(csvFile)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read CSV: %v", err)
	}

	// Verify header + data rows
	expectedRows := len(testWallets) + 1 // +1 for header
	if len(records) != expectedRows {
		t.Errorf("Expected %d CSV rows, got %d", expectedRows, len(records))
	}

	// Verify header
	if len(records) > 0 {
		header := records[0]
		if len(header) != 2 || header[0] != "address" || header[1] != "private_key" {
			t.Errorf("Invalid CSV header: %v", header)
		}
	}

	// Verify data rows
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) != 2 {
			t.Errorf("Row %d has %d columns, expected 2", i, len(row))
			continue
		}

		// Verify wallet is valid
		valid, err := VerifyExportedWallet(row[0], row[1])
		if err != nil {
			t.Errorf("Row %d verification failed: %v", i, err)
		}
		if !valid {
			t.Errorf("Row %d: address does not match private key", i)
		}
	}
}

// TestExporterConcurrency tests concurrent export operations
func TestExporterConcurrency(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.Config{
		ExportEnabled:       true,
		ExportMode:          "paired",
		ExportDir:           tmpDir,
		ExportOverwrite:     true,
		ExportAddressPrefix: true,
		ExportKeyPrefix:     true,
		ExportUseChecksum:   true,
	}

	exporter, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter() failed: %v", err)
	}
	defer exporter.Close()

	// Generate test wallets concurrently
	const numWallets = 100
	var wg sync.WaitGroup

	for i := 0; i < numWallets; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			w, err := Generate()
			if err != nil {
				t.Errorf("Generate() failed: %v", err)
				return
			}

			if err := exporter.Export(w.AddressHex(), "0x"+w.PrivateKeyHex()); err != nil {
				t.Errorf("Export() failed: %v", err)
			}
		}()
	}

	wg.Wait()

	if err := exporter.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify count
	if exporter.Count() != numWallets {
		t.Errorf("Expected %d wallets exported, got %d", numWallets, exporter.Count())
	}

	// Verify files have correct number of lines
	addressData, err := os.ReadFile(filepath.Join(tmpDir, "address.txt"))
	if err != nil {
		t.Fatalf("Failed to read address file: %v", err)
	}
	addressLines := strings.Split(strings.TrimSpace(string(addressData)), "\n")
	if len(addressLines) != numWallets {
		t.Errorf("Expected %d addresses, got %d", numWallets, len(addressLines))
	}

	keyData, err := os.ReadFile(filepath.Join(tmpDir, "privatekey.txt"))
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}
	keyLines := strings.Split(strings.TrimSpace(string(keyData)), "\n")
	if len(keyLines) != numWallets {
		t.Errorf("Expected %d keys, got %d", numWallets, len(keyLines))
	}
}

// TestExporterAppendMode tests append mode (non-overwrite)
func TestExporterAppendMode(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := config.Config{
		ExportEnabled:       true,
		ExportMode:          "paired",
		ExportDir:           tmpDir,
		ExportOverwrite:     false, // Append mode
		ExportAddressPrefix: true,
		ExportKeyPrefix:     true,
		ExportUseChecksum:   true,
	}

	// First export
	exporter1, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter() failed: %v", err)
	}

	if err := exporter1.Export("0x1111111111111111111111111111111111111111", "0x1111111111111111111111111111111111111111111111111111111111111111"); err != nil {
		t.Fatalf("Export() failed: %v", err)
	}

	if err := exporter1.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Second export (should append)
	exporter2, err := NewExporter(cfg)
	if err != nil {
		t.Fatalf("NewExporter() failed: %v", err)
	}

	if err := exporter2.Export("0x2222222222222222222222222222222222222222", "0x2222222222222222222222222222222222222222222222222222222222222222"); err != nil {
		t.Fatalf("Export() failed: %v", err)
	}

	if err := exporter2.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Verify both wallets are in the file
	addressData, err := os.ReadFile(filepath.Join(tmpDir, "address.txt"))
	if err != nil {
		t.Fatalf("Failed to read address file: %v", err)
	}
	addressLines := strings.Split(strings.TrimSpace(string(addressData)), "\n")
	if len(addressLines) != 2 {
		t.Errorf("Expected 2 addresses in append mode, got %d", len(addressLines))
	}
}

// TestExporterInvalidConfig tests error handling for invalid configurations
func TestExporterInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config config.Config
	}{
		{
			name: "export disabled",
			config: config.Config{
				ExportEnabled: false,
			},
		},
		{
			name: "invalid export mode",
			config: config.Config{
				ExportEnabled: true,
				ExportMode:    "invalid",
				ExportDir:     t.TempDir(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewExporter(tt.config)
			if err == nil {
				t.Error("NewExporter() expected error but got none")
			}
		})
	}
}
