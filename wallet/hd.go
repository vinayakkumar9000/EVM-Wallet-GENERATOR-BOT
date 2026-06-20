// Package wallet — HD wallet generation with BIP-39/BIP-44 support
package wallet

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	bip32 "github.com/tyler-smith/go-bip32"
	bip39 "github.com/tyler-smith/go-bip39"
)

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
