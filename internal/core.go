package src

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	bip32 "github.com/tyler-smith/go-bip32"
	bip39 "github.com/tyler-smith/go-bip39"
)

// ============================================================================
// Wallet Generation
// ============================================================================

// Wallet holds the raw binary representation of an EVM key-pair.
type Wallet struct {
	Address         []byte  // 20 bytes — Ethereum address (last 20 bytes of keccak256(pubkey))
	PrivateKey      []byte  // 32 bytes — raw secp256k1 scalar
	DerivationIndex *uint32 // Optional: BIP-44 derivation index (nil for random wallets)
	DerivationPath  string  // Optional: BIP-44 derivation path (empty for random wallets)
}

// Generate creates a new random EVM wallet.
// Uses crypto/rand internally via go-ethereum, which is cryptographically secure.
func GenerateWallet() (*Wallet, error) {
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

// ============================================================================
// HD Wallet Generation (BIP-39/BIP-44)
// ============================================================================

// MnemonicStrength represents the entropy strength for mnemonic generation
type MnemonicStrength int

const (
	// Mnemonic12Words generates a 12-word mnemonic (128-bit entropy)
	Mnemonic12Words MnemonicStrength = 128
	// Mnemonic24Words generates a 24-word mnemonic (256-bit entropy)
	Mnemonic24Words MnemonicStrength = 256
)

// HDWalletConfig holds configuration for HD wallet generation
type HDWalletConfig struct {
	Mnemonic   string // BIP-39 mnemonic phrase
	Passphrase string // Optional BIP-39 passphrase (empty string if not used)
	StartIndex uint32 // Starting derivation index
	Count      uint32 // Number of addresses to derive
}

// GenerateMnemonic creates a new BIP-39 mnemonic phrase
func GenerateMnemonic(strength MnemonicStrength) (string, error) {
	entropy, err := bip39.NewEntropy(int(strength))
	if err != nil {
		return "", fmt.Errorf("generate entropy: %w", err)
	}

	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", fmt.Errorf("generate mnemonic: %w", err)
	}

	return mnemonic, nil
}

// ValidateMnemonic checks if a mnemonic phrase is valid according to BIP-39
func ValidateMnemonic(mnemonic string) error {
	if !bip39.IsMnemonicValid(mnemonic) {
		return fmt.Errorf("invalid mnemonic: checksum verification failed")
	}
	return nil
}

// DeriveWalletsFromMnemonic derives multiple Ethereum wallets from a BIP-39 mnemonic
// using the standard Ethereum derivation path: m/44'/60'/0'/0/i
func DeriveWalletsFromMnemonic(config HDWalletConfig) ([]*Wallet, error) {
	// Validate mnemonic
	if err := ValidateMnemonic(config.Mnemonic); err != nil {
		return nil, err
	}

	// Generate seed from mnemonic
	seed := bip39.NewSeed(config.Mnemonic, config.Passphrase)

	// Create master key
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}

	// Derive wallets
	wallets := make([]*Wallet, 0, config.Count)

	for i := uint32(0); i < config.Count; i++ {
		index := config.StartIndex + i

		// Derive key at path m/44'/60'/0'/0/index
		// m/44' (purpose) / 60' (Ethereum) / 0' (account) / 0 (change) / index (address_index)
		key, err := deriveEthereumKey(masterKey, index)
		if err != nil {
			return nil, fmt.Errorf("derive key at index %d: %w", index, err)
		}

		// Convert to ECDSA private key
		privateKey, err := crypto.ToECDSA(key.Key)
		if err != nil {
			return nil, fmt.Errorf("convert to ECDSA at index %d: %w", index, err)
		}

		// Create wallet
		wallet, err := fromPrivateKey(privateKey, index)
		if err != nil {
			return nil, fmt.Errorf("create wallet at index %d: %w", index, err)
		}

		wallets = append(wallets, wallet)
	}

	return wallets, nil
}

// deriveEthereumKey derives a key at the Ethereum BIP-44 path: m/44'/60'/0'/0/index
func deriveEthereumKey(masterKey *bip32.Key, index uint32) (*bip32.Key, error) {
	// m/44'
	purpose, err := masterKey.NewChildKey(bip32.FirstHardenedChild + 44)
	if err != nil {
		return nil, fmt.Errorf("derive purpose: %w", err)
	}

	// m/44'/60'
	coinType, err := purpose.NewChildKey(bip32.FirstHardenedChild + 60)
	if err != nil {
		return nil, fmt.Errorf("derive coin type: %w", err)
	}

	// m/44'/60'/0'
	account, err := coinType.NewChildKey(bip32.FirstHardenedChild + 0)
	if err != nil {
		return nil, fmt.Errorf("derive account: %w", err)
	}

	// m/44'/60'/0'/0
	change, err := account.NewChildKey(0)
	if err != nil {
		return nil, fmt.Errorf("derive change: %w", err)
	}

	// m/44'/60'/0'/0/index
	addressKey, err := change.NewChildKey(index)
	if err != nil {
		return nil, fmt.Errorf("derive address: %w", err)
	}

	return addressKey, nil
}

// fromPrivateKey creates a wallet from an ECDSA private key with derivation index
func fromPrivateKey(privateKey *ecdsa.PrivateKey, derivationIndex uint32) (*Wallet, error) {
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("error casting public key to ECDSA")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	wallet := &Wallet{
		Address:         address.Bytes(),
		PrivateKey:      crypto.FromECDSA(privateKey),
		DerivationIndex: &derivationIndex,
		DerivationPath:  fmt.Sprintf("m/44'/60'/0'/0/%d", derivationIndex),
	}

	return wallet, nil
}

// GetDerivationPath returns the BIP-44 derivation path for Ethereum at a given index
func GetDerivationPath(index uint32) accounts.DerivationPath {
	return accounts.DerivationPath{
		bip32.FirstHardenedChild + 44, // purpose
		bip32.FirstHardenedChild + 60, // coin type (Ethereum)
		bip32.FirstHardenedChild + 0,  // account
		0,                             // change
		index,                         // address index
	}
}

// ============================================================================
// Wallet Import and Verification
// ============================================================================

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

// ============================================================================
// Vanity Address Matching
// ============================================================================

// VanityPattern represents a single vanity pattern (prefix and/or suffix)
type VanityPattern struct {
	Prefix string
	Suffix string
	Name   string // Optional name for the pattern
}

// MatchesVanity checks if an address matches the given prefix and/or suffix pattern.
// addr: the full address string (with or without 0x prefix)
// prefix: hex pattern to match at start (empty = no prefix check)
// suffix: hex pattern to match at end (empty = no suffix check)
// checksum: if true, performs case-sensitive match against EIP-55 checksummed address
// Returns (matches bool, matchedPattern int) where matchedPattern is -1 if no match
func MatchesVanity(addr string, prefix, suffix string, checksum bool) bool {
	matched, _ := MatchesVanitySingle(addr, prefix, suffix, checksum)
	return matched
}

// MatchesVanitySingle checks if an address matches a single prefix/suffix pattern.
// Returns (matches bool, empty string) for backward compatibility
func MatchesVanitySingle(addr string, prefix, suffix string, checksum bool) (bool, string) {
	// Strip 0x prefix if present
	addr = strings.TrimPrefix(addr, "0x")

	// Validate address length
	if len(addr) != 40 {
		return false, ""
	}

	// Get the comparison address based on checksum mode
	compareAddr := addr
	if checksum {
		// Use EIP-55 checksummed address for case-sensitive matching
		// Convert to common.Address and back to get proper checksum
		addrBytes := common.HexToAddress("0x" + addr)
		compareAddr = strings.TrimPrefix(addrBytes.Hex(), "0x")
	} else {
		// Case-insensitive: lowercase everything
		compareAddr = strings.ToLower(addr)
		prefix = strings.ToLower(prefix)
		suffix = strings.ToLower(suffix)
	}

	// Check prefix match
	if prefix != "" {
		if !strings.HasPrefix(compareAddr, prefix) {
			return false, ""
		}
	}

	// Check suffix match
	if suffix != "" {
		if !strings.HasSuffix(compareAddr, suffix) {
			return false, ""
		}
	}

	patternName := fmt.Sprintf("%s...%s", prefix, suffix)
	return true, patternName
}

// MatchesAnyPattern checks if an address matches any of the given patterns (OR logic).
// Returns (matches bool, patternIndex int, patternName string)
// patternIndex is -1 if no match, otherwise the index of the matched pattern
func MatchesAnyPattern(addr string, patterns []VanityPattern, checksum bool) (bool, int, string) {
	for i, pattern := range patterns {
		if matched, name := MatchesVanitySingle(addr, pattern.Prefix, pattern.Suffix, checksum); matched {
			displayName := name
			if pattern.Name != "" {
				displayName = pattern.Name
			}
			return true, i, displayName
		}
	}
	return false, -1, ""
}

// IsValidHexPattern validates that a pattern contains only valid hex characters.
// Returns true if pattern is empty or contains only [0-9a-fA-F].
func IsValidHexPattern(pattern string) bool {
	if pattern == "" {
		return true
	}

	for _, c := range pattern {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ValidateVanityPattern validates a vanity pattern and returns an error if invalid.
func ValidateVanityPattern(pattern string, name string) error {
	if pattern == "" {
		return nil // Empty is valid (means no constraint)
	}

	if !IsValidHexPattern(pattern) {
		return fmt.Errorf("%s contains invalid characters (must be hex: 0-9, a-f, A-F)", name)
	}

	if len(pattern) > 40 {
		return fmt.Errorf("%s is too long (max 40 characters)", name)
	}

	return nil
}

// CalculateDifficulty computes the expected number of attempts needed to find a match.
// Formula: base_difficulty = 16^(len(prefix) + len(suffix))
// If checksum mode: multiply by 2^(count of alphabetic hex chars) for case sensitivity
func CalculateDifficulty(prefix, suffix string, checksum bool) float64 {
	totalChars := len(prefix) + len(suffix)
	if totalChars == 0 {
		return 1.0 // No pattern = always matches
	}

	// Base difficulty: 16^n where n is total pattern length
	baseDifficulty := math.Pow(16, float64(totalChars))

	if !checksum {
		return baseDifficulty
	}

	// Checksum mode: count alphabetic hex chars (a-f, A-F)
	// Each alphabetic char doubles difficulty due to case sensitivity
	alphaCount := 0
	for _, c := range prefix + suffix {
		if (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			alphaCount++
		}
	}

	// Multiply by 2^alphaCount for case-sensitive matching
	return baseDifficulty * math.Pow(2, float64(alphaCount))
}

// CalculateMultiPatternDifficulty computes difficulty for multiple patterns (OR logic).
// For OR logic, the effective difficulty is based on the easiest pattern.
// Formula: 1/difficulty_total = 1/d1 + 1/d2 + ... + 1/dn
// Therefore: difficulty_total = 1 / (1/d1 + 1/d2 + ... + 1/dn)
func CalculateMultiPatternDifficulty(patterns []VanityPattern, checksum bool) float64 {
	if len(patterns) == 0 {
		return 1.0
	}

	if len(patterns) == 1 {
		return CalculateDifficulty(patterns[0].Prefix, patterns[0].Suffix, checksum)
	}

	// Calculate sum of reciprocals
	sumReciprocals := 0.0
	for _, pattern := range patterns {
		difficulty := CalculateDifficulty(pattern.Prefix, pattern.Suffix, checksum)
		if difficulty > 0 {
			sumReciprocals += 1.0 / difficulty
		}
	}

	if sumReciprocals == 0 {
		return 1.0
	}

	return 1.0 / sumReciprocals
}

// EstimateTime calculates time estimates for finding a match.
// Returns (time for 50% probability, time for 99% probability)
func EstimateTime(difficulty float64, speedPerSecond float64) (time50, time99 time.Duration) {
	if speedPerSecond <= 0 {
		return 0, 0
	}

	// 50% probability: difficulty * ln(2)
	attempts50 := difficulty * math.Ln2
	seconds50 := attempts50 / speedPerSecond
	time50 = time.Duration(seconds50 * float64(time.Second))

	// 99% probability: difficulty * ln(100)
	attempts99 := difficulty * math.Log(100)
	seconds99 := attempts99 / speedPerSecond
	time99 = time.Duration(seconds99 * float64(time.Second))

	return time50, time99
}

// CalculateProbability computes the probability of finding at least one match
// after K attempts given the difficulty.
// Formula: P = 1 - (1 - 1/difficulty)^attempts
func CalculateProbability(attempts int64, difficulty float64) float64 {
	if difficulty <= 0 {
		return 1.0
	}

	// For very large difficulties, use approximation to avoid numerical issues
	// P ≈ 1 - e^(-attempts/difficulty)
	if difficulty > 1e15 {
		return 1.0 - math.Exp(-float64(attempts)/difficulty)
	}

	// Exact formula: P = 1 - (1 - 1/difficulty)^attempts
	return 1.0 - math.Pow(1.0-1.0/difficulty, float64(attempts))
}

// FormatDifficulty formats a difficulty number with appropriate units.
func FormatDifficulty(difficulty float64) string {
	if difficulty < 1000 {
		return fmt.Sprintf("%.0f", difficulty)
	}
	if difficulty < 1e6 {
		return fmt.Sprintf("%.1fK", difficulty/1e3)
	}
	if difficulty < 1e9 {
		return fmt.Sprintf("%.1fM", difficulty/1e6)
	}
	if difficulty < 1e12 {
		return fmt.Sprintf("%.1fB", difficulty/1e9)
	}
	return fmt.Sprintf("%.1fT", difficulty/1e12)
}

// FormatSpeed formats addresses per second with appropriate units.
func FormatSpeed(speed float64) string {
	if speed < 1000 {
		return fmt.Sprintf("%.0f/s", speed)
	}
	if speed < 1e6 {
		return fmt.Sprintf("%.1fK/s", speed/1e3)
	}
	return fmt.Sprintf("%.1fM/s", speed/1e6)
}

// FormatDuration formats a duration in a human-readable way.
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	days := d.Hours() / 24
	if days < 365 {
		return fmt.Sprintf("%.1fd", days)
	}
	return fmt.Sprintf("%.1fy", days/365)
}

// ============================================================================
// Wallet Export
// ============================================================================

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
	config      Config
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
func NewExporter(cfg Config) (*Exporter, error) {
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

// ============================================================================
// Constants
// ============================================================================

const (
	// Progress update interval for terminal display
	ProgressUpdateInterval = 200 * time.Millisecond

	// Retry configuration
	RetryInitialDelay = 100 * time.Millisecond
	RetryMaxDelay     = 5 * time.Second

	// Batch processing delays
	BatchProcessDelay = 50 * time.Millisecond

	// Health check timeout
	HealthCheckTimeout = 5 * time.Minute

	// Connection pool monitoring interval
	PoolMonitorInterval = 30 * time.Second

	// Graceful shutdown grace period
	ShutdownGracePeriod = 2 * time.Second

	// Pool warmup configuration
	MinPoolWarmup    = 100 // Minimum objects to pre-allocate
	MaxPoolWarmup    = 256 // Maximum objects to pre-allocate
	WarmupMultiplier = 16  // Objects per CPU core
)

// ============================================================================
// Wallet Pool
// ============================================================================

// walletPool reuses wallet objects to reduce GC pressure.
// ponytail: sync.Pool is stdlib, no new dependency needed.
var walletPool = sync.Pool{
	New: func() interface{} {
		return &Wallet{
			Address:    make([]byte, 20),
			PrivateKey: make([]byte, 32),
		}
	},
}

// init pre-warms the wallet pool with objects to reduce initial allocation spike.
// ponytail: Dynamic warmup based on CPU cores.
// Ceiling: MaxPoolWarmup objects. Upgrade: make configurable if needed.
func init() {
	warmupSize := runtime.NumCPU() * WarmupMultiplier
	if warmupSize > MaxPoolWarmup {
		warmupSize = MaxPoolWarmup
	}
	if warmupSize < MinPoolWarmup {
		warmupSize = MinPoolWarmup
	}

	for i := 0; i < warmupSize; i++ {
		walletPool.Put(&Wallet{
			Address:    make([]byte, 20),
			PrivateKey: make([]byte, 32),
		})
	}
}

// ============================================================================
// Wallet Generation Engine
// ============================================================================

// GenerateWallets generates `totalWallets` EVM wallets in parallel, inserts them
// into the storage backend, and updates a single terminal line in-place.
func GenerateWallets(ctx context.Context, store Storage, cfg *Config, totalWallets int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()

	// ponytail: Auto-tune workers based on CPU cores (stdlib runtime package).
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > totalWallets {
		workers = totalWallets
	}

	// Initialize exporter if enabled
	var exporter *Exporter
	var exportErr error
	if cfg.ExportEnabled {
		exporter, exportErr = NewExporter(*cfg)
		if exportErr != nil {
			log.Printf("[WARN] Failed to initialize exporter: %v (continuing without export)", exportErr)
		} else {
			defer func() {
				if err := exporter.Close(); err != nil {
					log.Printf("[ERROR] Failed to close exporter: %v", err)
				}
			}()
			log.Printf("[INFO] Export enabled: mode=%s dir=%s", cfg.ExportMode, cfg.ExportDir)
		}
	}

	log.Printf("[INFO] Generating %d wallets | workers=%d (auto-tuned) | DB chunk=%d\n",
		totalWallets, workers, cfg.BatchSize)

	// ── Progress tracking ─────────────────────────────────────────────────
	var confirmedCount atomic.Int64
	progressDone := make(chan struct{})

	fmt.Printf("\n")
	tracker := NewProgressTracker(totalWallets)

	// Start progress rendering goroutine
	go func() {
		ticker := time.NewTicker(120 * time.Millisecond) // ~8 FPS for smooth animation
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tracker.Render(int(confirmedCount.Load()))
			case <-progressDone:
				tracker.Render(int(confirmedCount.Load()))
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// ── Batch event tracking (simplified from per-wallet events) ──────────
	// ponytail: Batch-level logging instead of per-wallet events
	var batchesCompleted atomic.Int64

	// ── Parallel key-generation goroutines with backpressure ──────────────
	// ponytail: Use bounded channel to prevent memory explosion
	// Worker pool + buffered channel provides natural backpressure
	walletCh := make(chan *Wallet, cfg.BatchSize*2)

	var genWG sync.WaitGroup
	perWorker := totalWallets / workers
	remainder := totalWallets % workers

	for i := 0; i < workers; i++ {
		count := perWorker
		if i < remainder {
			count++
		}
		genWG.Add(1)
		go func(n int, workerID int) {
			defer genWG.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[ERROR] Generator worker %d panic recovered: %v", workerID, r)
				}
			}()

			for j := 0; j < n; j++ {
				// Check context cancellation
				select {
				case <-ctx.Done():
					log.Printf("[INFO] Worker %d stopping due to context cancellation", workerID)
					return
				default:
				}

				// ponytail: Reuse wallet objects from pool to reduce GC pressure
				w := walletPool.Get().(*Wallet)
				if err := GenerateInto(w); err != nil {
					log.Printf("[WARN] Key generation error (skipping): %v", err)
					// DO NOT return corrupted object to pool
					continue
				}

				// Send with context cancellation check to prevent blocking during shutdown
				select {
				case walletCh <- w:
					// Successfully sent
				case <-ctx.Done():
					log.Printf("[INFO] Worker %d stopping during send (context cancelled)", workerID)
					return
				}
			}
		}(count, i)
	}

	go func() {
		genWG.Wait()
		close(walletCh)
	}()

	// ── Sequential batch inserter using COPY ──────────────────────────────
	batch := make([]*Wallet, 0, cfg.BatchSize)
	batchNum := 0

	for w := range walletCh {
		batch = append(batch, w)

		if len(batch) >= cfg.BatchSize {
			batchNum++

			// Retry storage insert with exponential backoff
			var ids []int64
			retryErr := WithRetry(ctx, DefaultRetryConfig(), func() error {
				var err error
				ids, err = store.SaveWallets(ctx, batch)
				return err
			})

			if retryErr != nil {
				close(progressDone)
				cancel()     // Cancel context to stop workers
				genWG.Wait() // Wait for all workers to finish
				return fmt.Errorf("storage insert (batch %d) failed after retries: %w", batchNum, retryErr)
			}
			confirmedCount.Add(int64(len(ids)))
			batchesCompleted.Add(1)

			// Export wallets if exporter is enabled
			if exporter != nil {
				for _, w := range batch {
					if err := exporter.Export(w.AddressHex(), "0x"+w.PrivateKeyHex()); err != nil {
						log.Printf("[WARN] Export failed for wallet: %v", err)
					}
				}
			}

			// Log batch completion (not per-wallet) - optional via config
			if cfg.EnableLogging {
				log.Printf("[INFO] Batch %d complete: %d wallets inserted", batchNum, len(ids))
			}

			// ponytail: Return wallet objects to pool for reuse.
			// Reset wallet data before returning to pool to prevent data leakage
			for _, w := range batch {
				// Clear sensitive data
				for i := range w.Address {
					w.Address[i] = 0
				}
				for i := range w.PrivateKey {
					w.PrivateKey[i] = 0
				}
				walletPool.Put(w)
			}
			batch = batch[:0]
		}
	}

	// ── Flush remainder ───────────────────────────────────────────────────
	if len(batch) > 0 {
		batchNum++

		// Retry storage insert with exponential backoff
		var ids []int64
		retryErr := WithRetry(ctx, DefaultRetryConfig(), func() error {
			var err error
			ids, err = store.SaveWallets(ctx, batch)
			return err
		})

		if retryErr != nil {
			close(progressDone)
			cancel()     // Cancel context to stop workers
			genWG.Wait() // Wait for all workers to finish
			return fmt.Errorf("storage insert (final batch) failed after retries: %w", retryErr)
		}
		confirmedCount.Add(int64(len(ids)))
		batchesCompleted.Add(1)

		// Export wallets if exporter is enabled
		if exporter != nil {
			for _, w := range batch {
				if err := exporter.Export(w.AddressHex(), "0x"+w.PrivateKeyHex()); err != nil {
					log.Printf("[WARN] Export failed for wallet: %v", err)
				}
			}
		}

		// Log batch completion (not per-wallet) - optional via config
		if cfg.EnableLogging {
			log.Printf("[INFO] Final batch %d complete: %d wallets inserted", batchNum, len(ids))
		}

		// Reset wallet data before returning to pool to prevent data leakage
		for _, w := range batch {
			// Clear sensitive data
			for i := range w.Address {
				w.Address[i] = 0
			}
			for i := range w.PrivateKey {
				w.PrivateKey[i] = 0
			}
			walletPool.Put(w)
		}
	}

	close(progressDone)
	time.Sleep(150 * time.Millisecond) // Let final render complete

	done := int(confirmedCount.Load())
	elapsed := time.Since(start)
	tracker.Finish(done)

	// Flush exporter and get export count
	var exportCount int
	if exporter != nil {
		if err := exporter.Flush(); err != nil {
			log.Printf("[WARN] Failed to flush exporter: %v", err)
		}
		exportCount = exporter.Count()
	}

	log.Printf("[INFO] Generation complete: %d wallets in %v (%.2f wallets/sec)",
		done, elapsed, float64(done)/elapsed.Seconds())

	if exportCount > 0 {
		log.Printf("[INFO] Export complete: %d wallets exported to %s (mode: %s)",
			exportCount, cfg.ExportDir, cfg.ExportMode)
	}

	return nil
}

// ============================================================================
// Benchmark
// ============================================================================

// BenchmarkWalletGeneration generates wallets for benchmarking WITHOUT storing them.
// This is used purely for performance measurement and tuning.
func BenchmarkWalletGeneration(ctx context.Context, cfg *Config, totalWallets int) (time.Duration, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()

	// Auto-tune workers based on CPU cores
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > totalWallets {
		workers = totalWallets
	}

	// Counter for generated wallets
	var generated atomic.Int64

	// Worker pool for parallel generation
	var wg sync.WaitGroup
	perWorker := totalWallets / workers
	remainder := totalWallets % workers

	for i := 0; i < workers; i++ {
		count := perWorker
		if i < remainder {
			count++
		}
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < n; j++ {
				// Check context cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Generate wallet (throwaway, not stored)
				w := walletPool.Get().(*Wallet)
				if err := GenerateInto(w); err != nil {
					walletPool.Put(w)
					continue
				}

				// Immediately return to pool - no storage
				generated.Add(1)
				walletPool.Put(w)
			}
		}(count)
	}

	// Wait for all workers to finish
	wg.Wait()

	elapsed := time.Since(start)
	return elapsed, nil
}

// ============================================================================
// Statistics
// ============================================================================

// GetStats queries statistics from the active storage backend.
func GetStats(ctx context.Context, store Storage) (*Stats, error) {
	return store.GetStats(ctx)
}

// PrintStats renders the statistics table to stdout.
func PrintStats(s *Stats) {
	line := "  ├─────────────────────────────────────────────────┤"
	top := "  ╔═════════════════════════════════════════════════╗"
	bot := "  ╚═════════════════════════════════════════════════╝"
	title := "  ║              WALLET STATISTICS                 ║"

	fmt.Println()
	fmt.Println(top)
	fmt.Println(title)
	fmt.Println(line)
	printRow("Total wallets", fmt.Sprintf("%d", s.TotalWallets))
	printRow("Wallets created today", fmt.Sprintf("%d", s.WalletsToday))
	printRow("Unused wallets", fmt.Sprintf("%d", s.UnusedWallets))
	printRow("Used wallets", fmt.Sprintf("%d", s.UsedWallets))
	fmt.Println(line)
	printRow("Total events logged", fmt.Sprintf("%d", s.TotalEvents))
	printRow("Database size", FormatBytes(s.DBSizeBytes))
	fmt.Println(line)

	if !s.NewestWallet.IsZero() {
		printRow("Last wallet created", s.NewestWallet.Format("2006-01-02 15:04:05"))
	} else {
		printRow("Last wallet created", "N/A — no wallets yet")
	}

	fmt.Println(bot)
	fmt.Println()
}

// ============================================================================
// Vanity Address Generation
// ============================================================================

// VanityConfig holds configuration for vanity address generation
type VanityConfig struct {
	Patterns    []VanityPattern // Multiple patterns (OR logic - match any)
	Checksum    bool
	TargetCount int

	// Legacy single-pattern support (deprecated, use Patterns instead)
	Prefix string
	Suffix string
}

// VanityStats tracks vanity generation statistics
type VanityStats struct {
	Attempts     atomic.Int64
	MatchesFound atomic.Int64
	StartTime    time.Time
	Speed        atomic.Uint64 // addresses per second (stored as uint64 for atomic ops)
	ResumeID     int64         // ID of resumed search session (0 if new)
}

// VanityMatch represents a found vanity wallet
type VanityMatch struct {
	Wallet       *Wallet
	Attempts     int64
	Elapsed      time.Duration
	PatternIndex int    // Index of the pattern that matched (-1 for legacy single pattern)
	PatternName  string // Name of the pattern that matched
}

// CalibrateSpeed measures wallet generation speed for 1 second
func CalibrateSpeed(ctx context.Context, cfg *Config) float64 {
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	var attempts atomic.Int64
	calibrationCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-calibrationCtx.Done():
					return
				default:
					w := walletPool.Get().(*Wallet)
					if err := GenerateInto(w); err == nil {
						attempts.Add(1)
					}
					walletPool.Put(w)
				}
			}
		}()
	}

	wg.Wait()
	return float64(attempts.Load())
}

// GenerateVanityWallets generates wallets matching the vanity pattern
func GenerateVanityWallets(ctx context.Context, _ Storage, cfg *Config, vanity VanityConfig) error {
	// Validate patterns
	if len(vanity.Patterns) > 0 {
		for i, p := range vanity.Patterns {
			if err := ValidateVanityPattern(p.Prefix, fmt.Sprintf("pattern %d prefix", i+1)); err != nil {
				return err
			}
			if err := ValidateVanityPattern(p.Suffix, fmt.Sprintf("pattern %d suffix", i+1)); err != nil {
				return err
			}
		}
	} else {
		// Legacy single-pattern validation
		if err := ValidateVanityPattern(vanity.Prefix, "prefix"); err != nil {
			return err
		}
		if err := ValidateVanityPattern(vanity.Suffix, "suffix"); err != nil {
			return err
		}

		// If both prefix and suffix are empty, return error
		if vanity.Prefix == "" && vanity.Suffix == "" {
			return fmt.Errorf("no vanity pattern specified")
		}
	}

	// Check for existing paused search (only for vanity.db storage)
	vanityStore, err := NewVanitySQLiteStorage(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("create vanity storage: %w", err)
	}
	defer vanityStore.Close()

	if err := vanityStore.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate vanity storage: %w", err)
	}

	existingSearch, err := vanityStore.GetActiveVanitySearchState(ctx)
	if err != nil {
		return fmt.Errorf("check for existing search: %w", err)
	}

	var resumeFrom *VanitySearchState
	if existingSearch != nil {
		fmt.Printf("\n  %s Found paused search: %d/%d matches, %s attempts\n",
			Info("ℹ"), existingSearch.MatchesFound, existingSearch.TargetCount,
			FormatNumber(int(existingSearch.Attempts)))
		fmt.Print("  Resume this search? [Y/n]: ")

		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))

		if response == "" || response == "y" || response == "yes" {
			resumeFrom = existingSearch
		} else {
			// Mark old search as completed and start fresh
			if err := vanityStore.MarkVanitySearchCompleted(ctx, existingSearch.ID); err != nil {
				log.Printf("[WARN] Failed to mark old search as completed: %v", err)
			}
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup Ctrl+C handler for graceful pause
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	start := time.Now()
	stats := &VanityStats{
		StartTime: start,
		ResumeID:  0,
	}

	// Calculate difficulty (needed for both new and resumed searches)
	var difficulty float64
	if len(vanity.Patterns) > 0 {
		difficulty = CalculateMultiPatternDifficulty(vanity.Patterns, vanity.Checksum)
	} else {
		difficulty = CalculateDifficulty(vanity.Prefix, vanity.Suffix, vanity.Checksum)
	}

	// If resuming, restore state
	if resumeFrom != nil {
		stats.Attempts.Store(resumeFrom.Attempts)
		stats.MatchesFound.Store(int64(resumeFrom.MatchesFound))
		stats.StartTime = resumeFrom.StartTime
		stats.ResumeID = resumeFrom.ID
		start = resumeFrom.StartTime

		fmt.Printf("  %s Resuming from %s attempts\n", Success("✓"), FormatNumber(int(resumeFrom.Attempts)))

		// Show difficulty and time estimates for resumed search
		speed := CalibrateSpeed(ctx, cfg)
		stats.Speed.Store(uint64(speed))

		// Calculate remaining difficulty based on attempts so far
		remainingTarget := vanity.TargetCount - resumeFrom.MatchesFound
		if remainingTarget > 0 {
			fmt.Printf("  %s Need %d more matches\n", Info("ℹ"), remainingTarget)
			fmt.Printf("  %s Current speed: ~%.0f addr/s\n", Info("ℹ"), speed)

			// Show time estimates for remaining matches
			time50, time99 := EstimateTime(difficulty, speed)
			fmt.Printf("  %s Estimated time per match: 50%% chance = %s, 99%% chance = %s\n\n",
				Info("ℹ"), FormatDuration(time50), FormatDuration(time99))
		}
	}

	// Calibrate speed (skip if resuming)
	if resumeFrom == nil {
		fmt.Print("\n  Calibrating speed... ")
		speed := CalibrateSpeed(ctx, cfg)
		stats.Speed.Store(uint64(speed))
		fmt.Printf("~%.0f addr/s\n", speed)

		// Display pre-flight panel
		showPreFlightPanel(vanity, difficulty, float64(stats.Speed.Load()))

		// Ask for confirmation if difficulty is high
		if !confirmVanityGeneration(difficulty, float64(stats.Speed.Load())) {
			return fmt.Errorf("generation cancelled by user")
		}
	}

	// Auto-tune workers
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	log.Printf("[INFO] Starting vanity generation | pattern=%s...%s | checksum=%v | workers=%d\n",
		vanity.Prefix, vanity.Suffix, vanity.Checksum, workers)

	// Channels for matches and progress
	matchCh := make(chan *VanityMatch, 10)
	progressDone := make(chan struct{})

	// Start progress display
	go displayVanityProgress(ctx, stats, vanity.TargetCount, difficulty, progressDone)

	// Start match collector
	matches := make([]*VanityMatch, 0, vanity.TargetCount)
	var matchMu sync.Mutex
	collectorDone := make(chan struct{})

	go func() {
		defer close(collectorDone)
		for match := range matchCh {
			matchMu.Lock()
			matches = append(matches, match)
			stats.MatchesFound.Store(int64(len(matches)))
			matchMu.Unlock()

			// Display match
			displayMatch(match, len(matches), vanity.TargetCount)

			// Check if we've found enough
			if len(matches) >= vanity.TargetCount {
				cancel() // Stop all workers
				return
			}
		}
	}()

	// Start periodic state saver (every 5 seconds)
	stateSaverDone := make(chan struct{})
	go func() {
		defer close(stateSaverDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := saveVanitySearchProgress(ctx, vanityStore, stats, vanity, resumeFrom); err != nil {
					log.Printf("[WARN] Failed to save search progress: %v", err)
				}
			case <-ctx.Done():
				// Final save on exit with fresh context (original may be cancelled)
				saveCtx := context.Background()
				if err := saveVanitySearchProgress(saveCtx, vanityStore, stats, vanity, resumeFrom); err != nil {
					log.Printf("[WARN] Failed to save final search progress: %v", err)
				}
				return
			}
		}
	}()

	// Start Ctrl+C handler for graceful pause
	go func() {
		select {
		case <-sigCh:
			fmt.Printf("\n\n  %s Ctrl+C detected, saving search state...\n", Warning("⚠️"))

			// Cancel context to stop workers
			cancel()

			// Save current state as "paused"
			if stats.ResumeID > 0 {
				// Update existing search
				if err := vanityStore.MarkVanitySearchPaused(context.Background(), stats.ResumeID); err != nil {
					log.Printf("[WARN] Failed to mark search as paused: %v", err)
				}
				if err := vanityStore.UpdateVanitySearchProgress(context.Background(), stats.ResumeID,
					stats.Attempts.Load(), int(stats.MatchesFound.Load())); err != nil {
					log.Printf("[WARN] Failed to save final progress: %v", err)
				}
			} else {
				// Save new search state
				if err := saveVanitySearchProgress(context.Background(), vanityStore, stats, vanity, resumeFrom); err != nil {
					log.Printf("[WARN] Failed to save search state: %v", err)
				}
				if stats.ResumeID > 0 {
					if err := vanityStore.MarkVanitySearchPaused(context.Background(), stats.ResumeID); err != nil {
						log.Printf("[WARN] Failed to mark search as paused: %v", err)
					}
				}
			}

			fmt.Printf("  %s Search paused. Run again to resume from %s attempts.\n\n",
				Success("✓"), FormatNumber(int(stats.Attempts.Load())))
		case <-ctx.Done():
			return
		}
	}()

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go vanityWorker(ctx, &wg, vanity, stats, matchCh, i)
	}

	// Wait for workers to finish
	wg.Wait()
	close(matchCh)

	// Wait for collector to finish
	<-collectorDone
	close(progressDone)

	// Final progress update
	time.Sleep(150 * time.Millisecond)
	fmt.Println()

	// Save matches to separate vanity database using fresh context
	// (original ctx may be cancelled after generation completes)
	if len(matches) > 0 {
		saveCtx := context.Background()
		if err := saveVanityMatches(saveCtx, nil, matches, cfg.DataDir); err != nil {
			log.Printf("[WARN] Failed to save matches to vanity.db: %v", err)
			fmt.Printf("  %s Failed to save to vanity.db, but generation completed successfully\n", Warning("⚠"))
		}
	}

	// Display summary
	displayVanitySummary(stats, matches, vanity.TargetCount, difficulty)

	return nil
}

// vanityWorker generates wallets and checks for vanity matches
func vanityWorker(ctx context.Context, wg *sync.WaitGroup, vanity VanityConfig, stats *VanityStats, matchCh chan<- *VanityMatch, workerID int) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] Vanity worker %d panic recovered: %v", workerID, r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Generate wallet
			w := walletPool.Get().(*Wallet)
			if err := GenerateInto(w); err != nil {
				walletPool.Put(w)
				continue
			}

			attempts := stats.Attempts.Add(1)
			addr := w.AddressHex()

			// Check if it matches any pattern
			var matched bool
			var patternIndex int
			var patternName string

			if len(vanity.Patterns) > 0 {
				// Multi-pattern mode
				matched, patternIndex, patternName = MatchesAnyPattern(addr, vanity.Patterns, vanity.Checksum)
			} else {
				// Legacy single-pattern mode
				matched = MatchesVanity(addr, vanity.Prefix, vanity.Suffix, vanity.Checksum)
				patternIndex = -1
				patternName = fmt.Sprintf("%s...%s", vanity.Prefix, vanity.Suffix)
			}

			if matched {
				// Found a match!
				match := &VanityMatch{
					Wallet:       w,
					Attempts:     attempts,
					Elapsed:      time.Since(stats.StartTime),
					PatternIndex: patternIndex,
					PatternName:  patternName,
				}

				select {
				case matchCh <- match:
					// Match sent successfully, don't return wallet to pool
					// It will be saved to database
				case <-ctx.Done():
					walletPool.Put(w)
					return
				}
			} else {
				// No match, return to pool
				walletPool.Put(w)
			}
		}
	}
}

// displayVanityProgress shows live progress with spinner
func displayVanityProgress(ctx context.Context, stats *VanityStats, target int, difficulty float64, done <-chan struct{}) {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	frame := 0
	lastAttempts := int64(0)
	lastTime := time.Now()

	for {
		select {
		case <-ticker.C:
			attempts := stats.Attempts.Load()
			matches := stats.MatchesFound.Load()

			// Calculate current speed
			now := time.Now()
			elapsed := now.Sub(lastTime).Seconds()
			if elapsed > 0 {
				currentSpeed := float64(attempts-lastAttempts) / elapsed
				stats.Speed.Store(uint64(currentSpeed))
				lastAttempts = attempts
				lastTime = now
			}

			speed := float64(stats.Speed.Load())
			prob := CalculateProbability(attempts, difficulty) * 100

			// Format output
			spinner := spinnerFrames[frame%len(spinnerFrames)]
			fmt.Printf("\r  %s  tried %s  ·  found %d/%d  ·  %s  ·  P≈%.1f%%",
				spinner,
				FormatNumber(int(attempts)),
				matches,
				target,
				FormatSpeed(speed),
				prob,
			)

			frame++

		case <-done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// displayMatch shows a found vanity wallet (only first 5 matches)
func displayMatch(match *VanityMatch, current, target int) {
	addr := match.Wallet.AddressHex()
	privKey := match.Wallet.PrivateKeyHex()

	// Clear progress line
	fmt.Print("\r" + clearLine())

	// Only display first 5 matches in terminal
	if current <= 5 {
		fmt.Printf("\n  ╔═══════════════════════════════════════════════════════════════╗\n")
		fmt.Printf("  ║ %s MATCH %d/%d%-47s║\n", Success("✓"), current, target, "")
		if match.PatternName != "" {
			patternInfo := fmt.Sprintf("Pattern: %s", match.PatternName)
			padding := 61 - len(patternInfo)
			if padding < 0 {
				padding = 0
			}
			fmt.Printf("  ║ %s%s║\n", Hint("%s", patternInfo), strings.Repeat(" ", padding))
		}
		fmt.Printf("  ╟───────────────────────────────────────────────────────────────╢\n")
		fmt.Printf("  ║ Address:     %-48s║\n", Info("%s", addr))
		fmt.Printf("  ║ Private Key: %-48s║\n", Hint("%s", privKey))
		fmt.Printf("  ╟───────────────────────────────────────────────────────────────╢\n")
		fmt.Printf("  ║ Attempts: %-15s  Elapsed: %-23s║\n",
			FormatNumber(int(match.Attempts)),
			match.Elapsed.Round(time.Millisecond).String())
		fmt.Printf("  ╚═══════════════════════════════════════════════════════════════╝\n\n")

		// Show message after 5th match
		if current == 5 && target > 5 {
			fmt.Printf("  %s Showing first 5 matches only. All %d wallets will be saved to vanity.db\n\n",
				Info("ℹ"), target)
		}
	} else if current == 6 {
		// Show progress indicator for remaining matches
		fmt.Printf("  %s Finding remaining matches... (%d/%d found)\n",
			Info("ℹ"), current, target)
	}
}

// showPreFlightPanel displays difficulty and time estimates
func showPreFlightPanel(vanity VanityConfig, difficulty float64, speed float64) {
	time50, time99 := EstimateTime(difficulty, speed)

	checksumMode := "case-insensitive"
	if vanity.Checksum {
		checksumMode = "case-sensitive (EIP-55)"
	}

	// Format pattern display
	var patternDisplay string
	if len(vanity.Patterns) > 1 {
		patternDisplay = fmt.Sprintf("%d patterns (OR logic)", len(vanity.Patterns))
	} else if len(vanity.Patterns) == 1 {
		p := vanity.Patterns[0]
		patternDisplay = fmt.Sprintf("0x%s……%s", p.Prefix, p.Suffix)
	} else {
		patternDisplay = fmt.Sprintf("0x%s……%s", vanity.Prefix, vanity.Suffix)
	}

	fmt.Printf(`
  ╔══════════════════════════════════════════════════════════════╗
  ║                   VANITY GENERATION PREVIEW                  ║
  ╠══════════════════════════════════════════════════════════════╣
  ║  Pattern(s)     : %-43s ║
  ║  Mode           : %-43s ║
  ║  Difficulty     : %-43s ║
  ║  Speed          : %-43s ║
  ║                                                              ║
  ║  Time Estimates:                                             ║
  ║    50%% chance   : %-43s ║
  ║    99%% chance   : %-43s ║
  ║                                                              ║
  ║  Workers        : %-43d ║
  ║  Target matches : %-43d ║
  ╚══════════════════════════════════════════════════════════════╝
`,
		patternDisplay,
		checksumMode,
		FormatDifficulty(difficulty),
		FormatSpeed(speed),
		FormatDuration(time50),
		FormatDuration(time99),
		runtime.NumCPU(),
		vanity.TargetCount,
	)

	// Show individual patterns if multiple
	if len(vanity.Patterns) > 1 {
		fmt.Println("\n  Patterns (match ANY):")
		for i, p := range vanity.Patterns {
			name := p.Name
			if name == "" {
				name = fmt.Sprintf("Pattern %d", i+1)
			}
			fmt.Printf("    %d. %s: 0x%s……%s\n", i+1, name, p.Prefix, p.Suffix)
		}
		fmt.Println()
	}
}

// confirmVanityGeneration asks user to confirm if difficulty is high
func confirmVanityGeneration(difficulty float64, speed float64) bool {
	time50, _ := EstimateTime(difficulty, speed)

	// Warn if estimated time is > 10 minutes
	if time50 > 10*time.Minute {
		fmt.Printf("\n  %s This may take a while (estimated: %s for 50%% chance)\n",
			Warning("⚠️"),
			FormatDuration(time50))
		fmt.Print("  Continue? [y/N]: ")

		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))

		return response == "y" || response == "yes"
	}

	return true
}

// saveVanityMatches saves found vanity wallets to separate vanity.db
func saveVanityMatches(ctx context.Context, _ Storage, matches []*VanityMatch, dataDir string) error {
	if len(matches) == 0 {
		return nil
	}

	// Create separate vanity storage
	vanityStore, err := NewVanitySQLiteStorage(dataDir)
	if err != nil {
		return fmt.Errorf("create vanity storage: %w", err)
	}
	defer vanityStore.Close()

	// Migrate schema
	if err := vanityStore.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate vanity storage: %w", err)
	}

	wallets := make([]*Wallet, len(matches))
	for i, match := range matches {
		wallets[i] = match.Wallet
	}

	_, err = vanityStore.SaveWallets(ctx, wallets)
	if err != nil {
		return fmt.Errorf("database insert failed: %w", err)
	}

	log.Printf("[INFO] Saved %d vanity wallets to vanity.db", len(matches))
	return nil
}

// displayVanitySummary shows final statistics
func displayVanitySummary(stats *VanityStats, matches []*VanityMatch, target int, difficulty float64) {
	attempts := stats.Attempts.Load()
	found := int64(len(matches))
	elapsed := time.Since(stats.StartTime)
	avgSpeed := float64(attempts) / elapsed.Seconds()

	fmt.Println()
	fmt.Println(Highlight("  ═══════════════════════════════════════════════════════════════"))
	fmt.Println(Success("  ✓ VANITY GENERATION COMPLETE"))
	fmt.Println(Highlight("  ═══════════════════════════════════════════════════════════════"))
	fmt.Println()
	fmt.Printf("  %s Matches found:  %d / %d\n", Info("ℹ"), found, target)
	fmt.Printf("  %s Total attempts: %s\n", Info("ℹ"), FormatNumber(int(attempts)))
	fmt.Printf("  %s Time elapsed:   %s\n", Info("ℹ"), elapsed.Round(time.Millisecond))
	fmt.Printf("  %s Average speed:  %s\n", Info("ℹ"), FormatSpeed(avgSpeed))
	fmt.Printf("  %s Difficulty:     %s\n", Info("ℹ"), FormatDifficulty(difficulty))
	fmt.Println()
	fmt.Println(Highlight("  ───────────────────────────────────────────────────────────────"))
	fmt.Printf("  %s All %d wallet(s) saved to vanity.db\n", Success("✓"), found)
	fmt.Println(Highlight("  ═══════════════════════════════════════════════════════════════"))
	fmt.Println()
}

// saveVanitySearchProgress saves current search progress to database
func saveVanitySearchProgress(ctx context.Context, store *SQLiteStorage, stats *VanityStats, vanity VanityConfig, resumeFrom *VanitySearchState) error {
	attempts := stats.Attempts.Load()
	matchesFound := int(stats.MatchesFound.Load())

	// Serialize patterns
	var patternsJSON string
	var err error
	if len(vanity.Patterns) > 0 {
		patternsJSON, err = SerializePatterns(vanity.Patterns)
	} else {
		// Legacy single pattern
		legacyPattern := []VanityPattern{{
			Prefix: vanity.Prefix,
			Suffix: vanity.Suffix,
			Name:   "Pattern 1",
		}}
		patternsJSON, err = SerializePatterns(legacyPattern)
	}
	if err != nil {
		return fmt.Errorf("serialize patterns: %w", err)
	}

	if resumeFrom != nil {
		// Update existing search
		return store.UpdateVanitySearchProgress(ctx, resumeFrom.ID, attempts, matchesFound)
	}

	// Create new search state
	state := &VanitySearchState{
		Patterns:     patternsJSON,
		Checksum:     vanity.Checksum,
		TargetCount:  vanity.TargetCount,
		Attempts:     attempts,
		MatchesFound: matchesFound,
		StartTime:    stats.StartTime,
		LastUpdate:   time.Now(),
		Status:       "active",
	}

	if err := store.SaveVanitySearchState(ctx, state); err != nil {
		return fmt.Errorf("save search state: %w", err)
	}

	// Update stats with new ID
	stats.ResumeID = state.ID
	return nil
}

// ============================================================================
// Database Health Monitoring
// ============================================================================

// HealthMetrics represents health statistics for a database table.
type HealthMetrics struct {
	TableName      string
	TotalSize      int64
	IndexSize      int64
	LiveTuples     int64
	DeadTuples     int64
	LastVacuum     *time.Time
	LastAutovacuum *time.Time
	BloatPercent   float64
}

// RunHealthCheck collects metrics, displays them, and records to database when supported.
func RunHealthCheck(ctx context.Context, store Storage) error {
	if err := store.HealthCheck(ctx); err != nil {
		return fmt.Errorf("storage health check: %w", err)
	}

	pgStore, ok := store.(*PostgresStorage)
	if !ok {
		stats, err := store.GetStats(ctx)
		if err != nil {
			return fmt.Errorf("load stats: %w", err)
		}
		fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │                STORAGE HEALTH                        │
  ├──────────────────────────────────────────────────────┤
  │  Backend          : %-33s │
  │  Status           : %-33s │
  │  Total wallets    : %-33d │
  │  Storage size     : %-33s │
  └──────────────────────────────────────────────────────┘
`,
			store.StorageType(),
			"healthy",
			stats.TotalWallets,
			FormatBytes(stats.DBSizeBytes),
		)
		log.Println("[INFO] Health check complete")
		return nil
	}

	log.Println("[INFO] Collecting database health metrics...")
	metrics, err := CollectHealthMetrics(ctx, pgStore.Pool())
	if err != nil {
		return fmt.Errorf("collect metrics: %w", err)
	}

	PrintHealthMetrics(metrics)

	log.Println("[INFO] Recording metrics to database_health table...")
	if err := RecordHealthMetrics(ctx, pgStore.Pool(), metrics); err != nil {
		return fmt.Errorf("record metrics: %w", err)
	}

	log.Println("[INFO] Health check complete")
	return nil
}

// CollectHealthMetrics queries PostgreSQL system catalogs for table health data.
func CollectHealthMetrics(ctx context.Context, pool *pgxpool.Pool) ([]HealthMetrics, error) {
	query := `
		SELECT 
			schemaname || '.' || relname AS table_name,
			pg_total_relation_size(relid) AS total_size,
			pg_indexes_size(relid) AS index_size,
			n_live_tup AS live_tuples,
			n_dead_tup AS dead_tuples,
			last_vacuum,
			last_autovacuum
		FROM pg_stat_user_tables
		WHERE schemaname = 'public'
		ORDER BY pg_total_relation_size(relid) DESC
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query health metrics: %w", err)
	}
	defer rows.Close()

	var metrics []HealthMetrics
	for rows.Next() {
		var m HealthMetrics
		err := rows.Scan(
			&m.TableName,
			&m.TotalSize,
			&m.IndexSize,
			&m.LiveTuples,
			&m.DeadTuples,
			&m.LastVacuum,
			&m.LastAutovacuum,
		)
		if err != nil {
			return nil, fmt.Errorf("scan health metrics: %w", err)
		}

		if m.LiveTuples > 0 {
			m.BloatPercent = float64(m.DeadTuples) / float64(m.LiveTuples+m.DeadTuples) * 100
		}

		metrics = append(metrics, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metrics, nil
}

// RecordHealthMetrics saves health metrics to database_health table for historical tracking.
func RecordHealthMetrics(ctx context.Context, pool *pgxpool.Pool, metrics []HealthMetrics) error {
	if len(metrics) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, m := range metrics {
		batch.Queue(`
			INSERT INTO database_health (
				table_name, total_size, index_size, 
				dead_tuples, live_tuples, 
				last_vacuum, last_autovacuum
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, m.TableName, m.TotalSize, m.IndexSize,
			m.DeadTuples, m.LiveTuples,
			m.LastVacuum, m.LastAutovacuum)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(metrics); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("record health metrics batch: %w", err)
		}
	}

	return nil
}

// PrintHealthMetrics displays health metrics in a formatted table.
func PrintHealthMetrics(metrics []HealthMetrics) {
	if len(metrics) == 0 {
		fmt.Println("\n[INFO] No health metrics available")
		return
	}

	top := "  ╔═══════════════════════════════════════════════════════════════════════════════╗"
	line := "  ├───────────────────────────────────────────────────────────────────────────────┤"
	bot := "  ╚═══════════════════════════════════════════════════════════════════════════════╝"
	title := "  ║                          DATABASE HEALTH METRICS                             ║"

	fmt.Println()
	fmt.Println(top)
	fmt.Println(title)
	fmt.Println(line)

	for _, m := range metrics {
		fmt.Printf("  ║  Table: %-68s ║\n", m.TableName)
		fmt.Println(line)
		fmt.Printf("  ║    Total Size      : %-54s ║\n", FormatBytes(m.TotalSize))
		fmt.Printf("  ║    Index Size      : %-54s ║\n", FormatBytes(m.IndexSize))
		fmt.Printf("  ║    Data Size       : %-54s ║\n", FormatBytes(m.TotalSize-m.IndexSize))
		fmt.Printf("  ║    Live Tuples     : %-54d ║\n", m.LiveTuples)
		fmt.Printf("  ║    Dead Tuples     : %-54d ║\n", m.DeadTuples)

		bloatStatus := fmt.Sprintf("%.1f%%", m.BloatPercent)
		if m.BloatPercent > 20 {
			bloatStatus += " ⚠️  HIGH - Consider VACUUM"
		} else if m.BloatPercent > 10 {
			bloatStatus += " ⚡ MODERATE"
		} else {
			bloatStatus += " ✓ HEALTHY"
		}
		fmt.Printf("  ║    Bloat           : %-54s ║\n", bloatStatus)

		if m.LastVacuum != nil {
			fmt.Printf("  ║    Last VACUUM     : %-54s ║\n", m.LastVacuum.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  ║    Last VACUUM     : %-54s ║\n", "Never")
		}

		if m.LastAutovacuum != nil {
			fmt.Printf("  ║    Last Autovacuum : %-54s ║\n", m.LastAutovacuum.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  ║    Last Autovacuum : %-54s ║\n", "Never")
		}

		fmt.Println(line)
	}

	fmt.Println(bot)
	fmt.Println()

	needsVacuum := false
	for _, m := range metrics {
		if m.BloatPercent > 20 {
			needsVacuum = true
			break
		}
	}

	if needsVacuum {
		log.Println("[WARN] Some tables have high bloat (>20%). Consider running VACUUM ANALYZE.")
		log.Println("[INFO] To vacuum: psql -d <database> -c 'VACUUM ANALYZE;'")
	}
}

// ============================================================================
// Retry Logic
// ============================================================================

// RetryConfig holds retry configuration parameters.
type RetryConfig struct {
	MaxAttempts  int           // Maximum number of retry attempts
	InitialDelay time.Duration // Initial delay between retries
	MaxDelay     time.Duration // Maximum delay between retries
	Multiplier   float64       // Backoff multiplier
}

// DefaultRetryConfig returns sensible defaults for database operations.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: RetryInitialDelay,
		MaxDelay:     RetryMaxDelay,
		Multiplier:   2.0,
	}
}

// RetryableFunc is a function that can be retried.
type RetryableFunc func() error

// WithRetry executes a function with exponential backoff retry logic.
// ponytail: Uses stdlib time package, no new dependencies.
func WithRetry(ctx context.Context, cfg RetryConfig, fn RetryableFunc) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context cancellation before attempting
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		default:
		}

		// Execute the function
		err := fn()
		if err == nil {
			// Success
			if attempt > 1 {
				log.Printf("[INFO] Operation succeeded on attempt %d/%d", attempt, cfg.MaxAttempts)
			}
			return nil
		}

		lastErr = err

		// Don't retry on last attempt
		if attempt == cfg.MaxAttempts {
			break
		}

		// Log retry attempt
		log.Printf("[WARN] Operation failed (attempt %d/%d): %v. Retrying in %v...",
			attempt, cfg.MaxAttempts, err, delay)

		// Wait with exponential backoff
		select {
		case <-time.After(delay):
			// Calculate next delay with exponential backoff
			delay = time.Duration(float64(delay) * cfg.Multiplier)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled during backoff: %w", ctx.Err())
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

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
