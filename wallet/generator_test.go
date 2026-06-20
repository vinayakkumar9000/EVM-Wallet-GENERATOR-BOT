package wallet

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestKnownVectors validates wallet generation against known test vectors.
// This ensures the crypto implementation is correct and hasn't regressed.
func TestKnownVectors(t *testing.T) {
	tests := []struct {
		name       string
		privateKey string
		wantAddr   string
	}{
		{
			name:       "Hardhat Account #0",
			privateKey: "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
			wantAddr:   "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		},
		{
			name:       "Hardhat Account #1",
			privateKey: "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
			wantAddr:   "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		},
		{
			name:       "Hardhat Account #2",
			privateKey: "0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
			wantAddr:   "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Remove 0x prefix
			privKeyHex := tt.privateKey[2:]

			// Decode private key
			privKeyBytes := common.Hex2Bytes(privKeyHex)
			privKey, err := crypto.ToECDSA(privKeyBytes)
			if err != nil {
				t.Fatalf("Failed to create ECDSA key: %v", err)
			}

			// Derive address
			gotAddr := crypto.PubkeyToAddress(privKey.PublicKey)

			// Compare addresses (case-insensitive)
			wantAddr := common.HexToAddress(tt.wantAddr)
			if gotAddr.Hex() != wantAddr.Hex() {
				t.Errorf("Address mismatch:\n  got:  %s\n  want: %s", gotAddr.Hex(), wantAddr.Hex())
			}
		})
	}
}

// TestDistinctKeys generates N fresh keys and ensures they are all distinct.
// This guards against a broken RNG or other crypto implementation issues.
func TestDistinctKeys(t *testing.T) {
	const numKeys = 10000

	addresses := make(map[string]bool, numKeys)
	privateKeys := make(map[string]bool, numKeys)

	for i := 0; i < numKeys; i++ {
		w, err := Generate()
		if err != nil {
			t.Fatalf("Failed to generate wallet %d: %v", i, err)
		}

		// Convert to hex strings for comparison
		addrHex := hex.EncodeToString(w.Address)
		privKeyHex := hex.EncodeToString(w.PrivateKey)

		// Check for duplicate address
		if addresses[addrHex] {
			t.Fatalf("Duplicate address found at iteration %d: %s", i, addrHex)
		}
		addresses[addrHex] = true

		// Check for duplicate private key
		if privateKeys[privKeyHex] {
			t.Fatalf("Duplicate private key found at iteration %d", i)
		}
		privateKeys[privKeyHex] = true
	}

	t.Logf("Successfully generated %d distinct wallets", numKeys)
}

// TestAddressDerivation ensures that address derivation is deterministic.
func TestAddressDerivation(t *testing.T) {
	// Generate a wallet
	w, err := Generate()
	if err != nil {
		t.Fatalf("Failed to generate wallet: %v", err)
	}

	// Re-derive address from private key
	privKey, err := crypto.ToECDSA(w.PrivateKey)
	if err != nil {
		t.Fatalf("Failed to create ECDSA key: %v", err)
	}

	derivedAddr := crypto.PubkeyToAddress(privKey.PublicKey)

	// Compare with original address (as bytes)
	if hex.EncodeToString(w.Address) != hex.EncodeToString(derivedAddr.Bytes()) {
		t.Errorf("Address derivation mismatch:\n  original: %s\n  derived:  %s",
			hex.EncodeToString(w.Address), hex.EncodeToString(derivedAddr.Bytes()))
	}
}

// TestWalletStructIntegrity ensures address and private key are always paired correctly.
func TestWalletStructIntegrity(t *testing.T) {
	const numTests = 1000

	for i := 0; i < numTests; i++ {
		w, err := Generate()
		if err != nil {
			t.Fatalf("Failed to generate wallet %d: %v", i, err)
		}

		// Verify the wallet's address matches its private key
		privKey, err := crypto.ToECDSA(w.PrivateKey)
		if err != nil {
			t.Fatalf("Invalid private key at iteration %d: %v", i, err)
		}

		expectedAddr := crypto.PubkeyToAddress(privKey.PublicKey)
		if hex.EncodeToString(w.Address) != hex.EncodeToString(expectedAddr.Bytes()) {
			t.Errorf("Wallet struct integrity violation at iteration %d:\n  wallet.Address: %s\n  derived from key: %s",
				i, hex.EncodeToString(w.Address), hex.EncodeToString(expectedAddr.Bytes()))
		}
	}

	t.Logf("Verified integrity of %d wallet structs", numTests)
}

// BenchmarkGenerate measures wallet generation performance.
func BenchmarkGenerate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := Generate()
		if err != nil {
			b.Fatalf("Failed to generate wallet: %v", err)
		}
	}
}
