// Package wallet - vanity address matching and validation
package wallet

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

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

// VanityPattern represents a single vanity pattern (prefix and/or suffix)
type VanityPattern struct {
	Prefix string
	Suffix string
	Name   string // Optional name for the pattern
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
