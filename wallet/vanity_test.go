package wallet

import (
	"testing"
)

func TestMatchesVanity(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		prefix   string
		suffix   string
		checksum bool
		want     bool
	}{
		// Case-insensitive tests
		{
			name:     "prefix match case-insensitive",
			addr:     "0xdeadbeef1234567890123456789012345678abcd",
			prefix:   "dead",
			suffix:   "",
			checksum: false,
			want:     true,
		},
		{
			name:     "suffix match case-insensitive",
			addr:     "0x1234567890123456789012345678901234abcdef",
			prefix:   "",
			suffix:   "cdef",
			checksum: false,
			want:     true,
		},
		{
			name:     "both prefix and suffix match case-insensitive",
			addr:     "0xdead567890123456789012345678901234abbeef",
			prefix:   "dead",
			suffix:   "beef",
			checksum: false,
			want:     true,
		},
		{
			name:     "prefix no match case-insensitive",
			addr:     "0x1234567890123456789012345678901234567890",
			prefix:   "dead",
			suffix:   "",
			checksum: false,
			want:     false,
		},
		{
			name:     "suffix no match case-insensitive",
			addr:     "0x1234567890123456789012345678901234567890",
			prefix:   "",
			suffix:   "beef",
			checksum: false,
			want:     false,
		},
		{
			name:     "case insensitive uppercase pattern",
			addr:     "0xdeadbeef1234567890123456789012345678abcd",
			prefix:   "DEAD",
			suffix:   "",
			checksum: false,
			want:     true,
		},
		{
			name:     "case insensitive mixed case address",
			addr:     "0xDeAdBeEf1234567890123456789012345678AbCd",
			prefix:   "dead",
			suffix:   "abcd",
			checksum: false,
			want:     true,
		},
		{
			name:     "empty prefix and suffix matches all",
			addr:     "0x1234567890123456789012345678901234567890",
			prefix:   "",
			suffix:   "",
			checksum: false,
			want:     true,
		},
		// Case-sensitive (checksum) tests
		{
			name:     "checksum mode exact match",
			addr:     "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
			prefix:   "5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
			suffix:   "",
			checksum: true,
			want:     true,
		},
		{
			name:     "checksum mode case mismatch",
			addr:     "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
			prefix:   "5aaeb6053f3e94c9b9a09f33669435e7ef1beaed",
			suffix:   "",
			checksum: true,
			want:     false,
		},
		{
			name:     "checksum mode suffix match",
			addr:     "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
			prefix:   "",
			suffix:   "BeAed",
			checksum: true,
			want:     true,
		},
		{
			name:     "checksum mode suffix case mismatch",
			addr:     "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
			prefix:   "",
			suffix:   "beaed",
			checksum: true,
			want:     false,
		},
		// Edge cases
		{
			name:     "address without 0x prefix",
			addr:     "deadbeef1234567890123456789012345678abcd",
			prefix:   "dead",
			suffix:   "",
			checksum: false,
			want:     true,
		},
		{
			name:     "single character prefix",
			addr:     "0xa234567890123456789012345678901234567890",
			prefix:   "a",
			suffix:   "",
			checksum: false,
			want:     true,
		},
		{
			name:     "single character suffix",
			addr:     "0x1234567890123456789012345678901234567890",
			prefix:   "",
			suffix:   "0",
			checksum: false,
			want:     true,
		},
		{
			name:     "full address match",
			addr:     "0x1234567890123456789012345678901234567890",
			prefix:   "1234567890123456789012345678901234567890",
			suffix:   "",
			checksum: false,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesVanity(tt.addr, tt.prefix, tt.suffix, tt.checksum)
			if got != tt.want {
				t.Errorf("MatchesVanity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidHexPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"empty pattern", "", true},
		{"valid lowercase", "deadbeef", true},
		{"valid uppercase", "DEADBEEF", true},
		{"valid mixed case", "DeAdBeEf", true},
		{"valid numbers", "12345678", true},
		{"valid hex chars", "0123456789abcdefABCDEF", true},
		{"invalid char g", "deadbeeg", false},
		{"invalid char z", "deadbeez", false},
		{"invalid special char", "dead-beef", false},
		{"invalid space", "dead beef", false},
		{"invalid 0x prefix", "0xdeadbeef", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidHexPattern(tt.pattern)
			if got != tt.want {
				t.Errorf("IsValidHexPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestValidateVanityPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		field   string
		wantErr bool
	}{
		{"empty pattern", "", "prefix", false},
		{"valid pattern", "dead", "prefix", false},
		{"valid long pattern", "deadbeef12345678", "suffix", false},
		{"invalid hex char", "deadbeeg", "prefix", true},
		{"too long", "12345678901234567890123456789012345678901", "prefix", true},
		{"max length ok", "1234567890123456789012345678901234567890", "prefix", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVanityPattern(tt.pattern, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVanityPattern() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateDifficulty(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		suffix   string
		checksum bool
		want     float64
	}{
		{"no pattern", "", "", false, 1.0},
		{"1 char case-insensitive", "a", "", false, 16.0},
		{"2 chars case-insensitive", "ab", "", false, 256.0},
		{"3 chars case-insensitive", "abc", "", false, 4096.0},
		{"prefix and suffix", "ab", "cd", false, 65536.0},       // 16^4
		{"1 alpha char checksum", "a", "", true, 32.0},          // 16 * 2^1
		{"2 alpha chars checksum", "ab", "", true, 1024.0},      // 256 * 2^2
		{"mixed alpha numeric checksum", "a1", "", true, 512.0}, // 256 * 2^1
		{"all numeric checksum", "12", "", false, 256.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateDifficulty(tt.prefix, tt.suffix, tt.checksum)
			if got != tt.want {
				t.Errorf("CalculateDifficulty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateProbability(t *testing.T) {
	tests := []struct {
		name       string
		attempts   int64
		difficulty float64
		wantMin    float64
		wantMax    float64
	}{
		{"zero attempts", 0, 100, 0.0, 0.01},
		{"one attempt easy", 1, 10, 0.09, 0.11},
		{"half difficulty", 50, 100, 0.39, 0.41},
		{"equal to difficulty", 100, 100, 0.63, 0.64},
		{"very high difficulty", 1000, 1e15, 0.0, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateProbability(tt.attempts, tt.difficulty)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CalculateProbability() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestFormatDifficulty(t *testing.T) {
	tests := []struct {
		name       string
		difficulty float64
		want       string
	}{
		{"small", 100, "100"},
		{"thousands", 5000, "5.0K"},
		{"millions", 5000000, "5.0M"},
		{"billions", 5000000000, "5.0B"},
		{"trillions", 5000000000000, "5.0T"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDifficulty(tt.difficulty)
			if got != tt.want {
				t.Errorf("FormatDifficulty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatSpeed(t *testing.T) {
	tests := []struct {
		name  string
		speed float64
		want  string
	}{
		{"low", 100, "100/s"},
		{"thousands", 5000, "5.0K/s"},
		{"millions", 5000000, "5.0M/s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSpeed(tt.speed)
			if got != tt.want {
				t.Errorf("FormatSpeed() = %v, want %v", got, tt.want)
			}
		})
	}
}
