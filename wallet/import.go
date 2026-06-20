// Package wallet provides import and verification functionality for private keys.
package wallet

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ImportResult contains the result of importing a private key
type ImportResult struct {
	PrivateKey string // Normalized private key (with 0x prefix)
	Address    string // EIP-55 checksum address
	Valid      bool   // Whether the key is valid
	Error      string // Error message if invalid
}

// ImportPrivateKey imports and verifies a private key, returning the derived address.
// Accepts keys with or without 0x prefix.
// Returns EIP-55 checksum address if valid.
func ImportPrivateKey(privateKeyInput string) *ImportResult {
	result := &ImportResult{
		Valid: false,
	}

	// Normalize input: trim whitespace
	privateKeyInput = strings.TrimSpace(privateKeyInput)
	if privateKeyInput == "" {
		result.Error = "Private key cannot be empty"
		return result
	}

	// Remove 0x prefix if present
	privateKey := privateKeyInput
	if strings.HasPrefix(strings.ToLower(privateKey), "0x") {
		privateKey = privateKey[2:]
	}

	// Validate hex string
	if len(privateKey) != 64 {
		result.Error = fmt.Sprintf("Invalid private key length: expected 64 hex characters, got %d", len(privateKey))
		return result
	}

	// Check if valid hex
	keyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		result.Error = fmt.Sprintf("Invalid hex string: %v", err)
		return result
	}

	// Validate key length (should be 32 bytes)
	if len(keyBytes) != 32 {
		result.Error = fmt.Sprintf("Invalid key length: expected 32 bytes, got %d", len(keyBytes))
		return result
	}

	// Create ECDSA private key
	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		result.Error = fmt.Sprintf("Invalid private key: %v", err)
		return result
	}

	// Derive address from private key
	address := crypto.PubkeyToAddress(privKey.PublicKey)

	// Return result with EIP-55 checksum address
	result.Valid = true
	result.PrivateKey = "0x" + privateKey
	result.Address = address.Hex() // This returns EIP-55 checksum format
	result.Error = ""

	return result
}

// ValidatePrivateKey checks if a private key string is valid without deriving the address.
// Returns true if the key is valid hex and correct length.
func ValidatePrivateKey(privateKeyInput string) (bool, error) {
	// Normalize input
	privateKeyInput = strings.TrimSpace(privateKeyInput)
	if privateKeyInput == "" {
		return false, fmt.Errorf("private key cannot be empty")
	}

	// Remove 0x prefix if present
	privateKey := privateKeyInput
	if strings.HasPrefix(strings.ToLower(privateKey), "0x") {
		privateKey = privateKey[2:]
	}

	// Validate length
	if len(privateKey) != 64 {
		return false, fmt.Errorf("invalid length: expected 64 hex characters, got %d", len(privateKey))
	}

	// Check if valid hex
	keyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return false, fmt.Errorf("invalid hex string: %v", err)
	}

	// Validate key length
	if len(keyBytes) != 32 {
		return false, fmt.Errorf("invalid key length: expected 32 bytes, got %d", len(keyBytes))
	}

	// Try to create ECDSA key
	_, err = crypto.ToECDSA(keyBytes)
	if err != nil {
		return false, fmt.Errorf("invalid private key: %v", err)
	}

	return true, nil
}

// DeriveAddress derives the Ethereum address from a private key.
// Returns EIP-55 checksum address.
func DeriveAddress(privateKeyHex string) (string, error) {
	// Remove 0x prefix if present
	if strings.HasPrefix(strings.ToLower(privateKeyHex), "0x") {
		privateKeyHex = privateKeyHex[2:]
	}

	// Decode hex
	keyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return "", fmt.Errorf("decode hex: %w", err)
	}

	// Create ECDSA key
	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return "", fmt.Errorf("create ECDSA key: %w", err)
	}

	// Derive address
	address := crypto.PubkeyToAddress(privKey.PublicKey)
	return address.Hex(), nil
}

// VerifyKeyAddressPair verifies that a private key matches an address.
// Both inputs can have or omit 0x prefix.
func VerifyKeyAddressPair(privateKeyHex, addressHex string) (bool, error) {
	// Derive address from private key
	derivedAddress, err := DeriveAddress(privateKeyHex)
	if err != nil {
		return false, fmt.Errorf("derive address: %w", err)
	}

	// Normalize expected address
	expectedAddress := strings.TrimSpace(addressHex)
	if !strings.HasPrefix(strings.ToLower(expectedAddress), "0x") {
		expectedAddress = "0x" + expectedAddress
	}

	// Compare addresses (case-insensitive)
	derivedAddr := common.HexToAddress(derivedAddress)
	expectedAddr := common.HexToAddress(expectedAddress)

	return derivedAddr == expectedAddr, nil
}
