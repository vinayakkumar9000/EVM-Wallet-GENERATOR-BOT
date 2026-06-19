package wallet

import (
	"encoding/hex"
	"testing"
)

// TestGenerate verifies basic wallet generation functionality
func TestGenerate(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}

	// Verify address is 20 bytes
	if len(w.Address) != 20 {
		t.Errorf("Address length = %d, want 20", len(w.Address))
	}

	// Verify private key is 32 bytes
	if len(w.PrivateKey) != 32 {
		t.Errorf("PrivateKey length = %d, want 32", len(w.PrivateKey))
	}

	// Verify address is not all zeros
	allZeros := true
	for _, b := range w.Address {
		if b != 0 {
			allZeros = false
			break
		}
	}
	if allZeros {
		t.Error("Address is all zeros")
	}

	// Verify private key is not all zeros
	allZeros = true
	for _, b := range w.PrivateKey {
		if b != 0 {
			allZeros = false
			break
		}
	}
	if allZeros {
		t.Error("PrivateKey is all zeros")
	}
}

// TestGenerateInto verifies wallet generation into existing object
func TestGenerateInto(t *testing.T) {
	w := &Wallet{
		Address:    make([]byte, 20),
		PrivateKey: make([]byte, 32),
	}

	err := GenerateInto(w)
	if err != nil {
		t.Fatalf("GenerateInto() failed: %v", err)
	}

	// Verify address is 20 bytes
	if len(w.Address) != 20 {
		t.Errorf("Address length = %d, want 20", len(w.Address))
	}

	// Verify private key is 32 bytes
	if len(w.PrivateKey) != 32 {
		t.Errorf("PrivateKey length = %d, want 32", len(w.PrivateKey))
	}

	// Verify data was written
	allZeros := true
	for _, b := range w.Address {
		if b != 0 {
			allZeros = false
			break
		}
	}
	if allZeros {
		t.Error("Address is all zeros after GenerateInto")
	}
}

// TestGenerateIntoReuse verifies object reuse works correctly
func TestGenerateIntoReuse(t *testing.T) {
	w := &Wallet{
		Address:    make([]byte, 20),
		PrivateKey: make([]byte, 32),
	}

	// Generate first wallet
	if err := GenerateInto(w); err != nil {
		t.Fatalf("First GenerateInto() failed: %v", err)
	}
	firstAddress := make([]byte, 20)
	copy(firstAddress, w.Address)

	// Generate second wallet into same object
	if err := GenerateInto(w); err != nil {
		t.Fatalf("Second GenerateInto() failed: %v", err)
	}

	// Verify addresses are different (extremely unlikely to be same)
	same := true
	for i := range w.Address {
		if w.Address[i] != firstAddress[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("Second generation produced identical address (extremely unlikely)")
	}
}

// TestAddressHex verifies hex encoding of address
func TestAddressHex(t *testing.T) {
	w := &Wallet{
		Address: []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
			0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
			0x12, 0x34, 0x56, 0x78},
		PrivateKey: make([]byte, 32),
	}

	hexAddr := w.AddressHex()
	if hexAddr[:2] != "0x" {
		t.Errorf("AddressHex() doesn't start with 0x: %s", hexAddr)
	}

	// Verify length (0x + 40 hex chars)
	if len(hexAddr) != 42 {
		t.Errorf("AddressHex() length = %d, want 42", len(hexAddr))
	}
}

// TestPrivateKeyHex verifies hex encoding of private key
func TestPrivateKeyHex(t *testing.T) {
	w := &Wallet{
		Address: make([]byte, 20),
		PrivateKey: []byte{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
			0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
		},
	}

	hexKey := w.PrivateKeyHex()

	// Verify length (64 hex chars)
	if len(hexKey) != 64 {
		t.Errorf("PrivateKeyHex() length = %d, want 64", len(hexKey))
	}

	// Verify it's valid hex
	_, err := hex.DecodeString(hexKey)
	if err != nil {
		t.Errorf("PrivateKeyHex() produced invalid hex: %v", err)
	}
}

// TestShortAddress verifies short address formatting
func TestShortAddress(t *testing.T) {
	w := &Wallet{
		Address: []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
			0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0,
			0x12, 0x34, 0x56, 0x78},
		PrivateKey: make([]byte, 32),
	}

	short := w.ShortAddress()

	// Should contain ellipsis
	if len(short) < 10 {
		t.Errorf("ShortAddress() too short: %s", short)
	}
}

// TestNormalizeAddress verifies address normalization
func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid with 0x prefix",
			input:   "0x1234567890123456789012345678901234567890",
			wantErr: false,
		},
		{
			name:    "valid without 0x prefix",
			input:   "1234567890123456789012345678901234567890",
			wantErr: false,
		},
		{
			name:    "too short",
			input:   "0x12345678",
			wantErr: true,
		},
		{
			name:    "too long",
			input:   "0x12345678901234567890123456789012345678901234",
			wantErr: true,
		},
		{
			name:    "invalid hex",
			input:   "0x123456789012345678901234567890123456789g",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := NormalizeAddress(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(addr) != 20 {
				t.Errorf("NormalizeAddress() returned %d bytes, want 20", len(addr))
			}
		})
	}
}

// TestStatusLabel verifies status label formatting
func TestStatusLabel(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{0, "unused"},
		{1, "used"},
		{2, "reserved"},
		{99, "status_99"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := StatusLabel(tt.status)
			if got != tt.want {
				t.Errorf("StatusLabel(%d) = %s, want %s", tt.status, got, tt.want)
			}
		})
	}
}

// BenchmarkGenerate measures wallet generation performance
func BenchmarkGenerate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := Generate()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateInto measures wallet generation with object reuse
func BenchmarkGenerateInto(b *testing.B) {
	w := &Wallet{
		Address:    make([]byte, 20),
		PrivateKey: make([]byte, 32),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := GenerateInto(w)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAddressHex measures hex encoding performance
func BenchmarkAddressHex(b *testing.B) {
	w := &Wallet{
		Address:    make([]byte, 20),
		PrivateKey: make([]byte, 32),
	}
	GenerateInto(w)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.AddressHex()
	}
}
