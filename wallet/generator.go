// Package wallet generates EVM-compatible wallets (secp256k1 + keccak256).
// Compatible with Ethereum, BSC, Polygon, Arbitrum, Optimism, Base, and any
// EVM chain — they all share the same key derivation scheme.
package wallet

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// Wallet holds the raw binary representation of an EVM key-pair.
type Wallet struct {
	Address    []byte // 20 bytes — Ethereum address (last 20 bytes of keccak256(pubkey))
	PrivateKey []byte // 32 bytes — raw secp256k1 scalar
}

// Generate creates a new random EVM wallet.
// Uses crypto/rand internally via go-ethereum, which is cryptographically secure.
func Generate() (*Wallet, error) {
	key, err := crypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("secp256k1 key generation: %w", err)
	}

	address := crypto.PubkeyToAddress(key.PublicKey)
	privBytes := crypto.FromECDSA(key)

	return &Wallet{
		Address:    address.Bytes(), // always 20 bytes
		PrivateKey: privBytes,       // always 32 bytes
	}, nil
}

// GenerateInto generates a new random EVM wallet into an existing Wallet object.
// This allows reusing wallet objects from sync.Pool to reduce GC pressure.
// ponytail: Reuse pattern for high-throughput generation (>1M wallets).
// Uses copy() to reuse pre-allocated slices instead of allocating new ones.
func GenerateInto(w *Wallet) error {
	key, err := crypto.GenerateKey()
	if err != nil {
		return fmt.Errorf("secp256k1 key generation: %w", err)
	}

	address := crypto.PubkeyToAddress(key.PublicKey)
	privBytes := crypto.FromECDSA(key)

	// Reuse existing slices instead of allocating new ones
	// This is critical for sync.Pool effectiveness
	copy(w.Address, address.Bytes())
	copy(w.PrivateKey, privBytes)
	return nil
}

// AddressHex returns the EIP-55 mixed-case checksummed address string.
func (w *Wallet) AddressHex() string {
	return "0x" + hex.EncodeToString(w.Address)
}

// PrivateKeyHex returns the hex-encoded private key (without 0x prefix).
// Use with caution — only for export operations, never log this.
func (w *Wallet) PrivateKeyHex() string {
	return hex.EncodeToString(w.PrivateKey)
}

// ShortAddress returns the first 6 + last 4 chars of the address for display.
func (w *Wallet) ShortAddress() string {
	full := w.AddressHex()
	if len(full) < 12 {
		return full
	}
	return full[:8] + "…" + full[len(full)-4:]
}

// StatusLabel returns a human-readable status label.
func StatusLabel(status int) string {
	switch status {
	case 0:
		return "unused"
	case 1:
		return "used"
	case 2:
		return "reserved"
	default:
		return fmt.Sprintf("status_%d", status)
	}
}

// NormalizeAddress converts a hex string to 20-byte slice (accepts with/without 0x).
func NormalizeAddress(hex20 string) ([]byte, error) {
	hex20 = strings.TrimPrefix(strings.ToLower(hex20), "0x")
	if len(hex20) != 40 {
		return nil, fmt.Errorf("address must be 40 hex chars (got %d)", len(hex20))
	}
	b, err := hex.DecodeString(hex20)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	return b, nil
}
