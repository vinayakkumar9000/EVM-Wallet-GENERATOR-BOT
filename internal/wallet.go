package internal

import (
	"bufio"
	"crypto/ecdsa"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
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
