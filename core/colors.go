// Package core — ANSI color utilities with TTY detection and NO_COLOR support.
package core

import (
	"os"
	"runtime"

	"golang.org/x/term"
)

// ANSI color codes
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	
	// Foreground colors
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Gray    = "\033[90m"
	
	// Background colors
	BgRed    = "\033[41m"
	BgGreen  = "\033[42m"
	BgYellow = "\033[43m"
)

// Screen control codes
const (
	ClearScreen = "\033[2J\033[H"
	ClearLine   = "\033[K"
)

var (
	// colorsEnabled is set once at init based on TTY detection and NO_COLOR
	colorsEnabled bool
)

func init() {
	// Check if colors should be enabled
	colorsEnabled = shouldEnableColors()
}

// shouldEnableColors determines if ANSI colors should be used.
// Returns false if:
// - NO_COLOR environment variable is set (any value)
// - stdout is not a TTY (piped/redirected)
// - Running on Windows without ANSI support (pre-Windows 10)
func shouldEnableColors() bool {
	// Honor NO_COLOR environment variable
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	// Check if stdout is a terminal
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}

	// On Windows, check for ANSI support
	if runtime.GOOS == "windows" {
		// Windows 10+ supports ANSI escape codes
		// For older versions, we'd need to enable virtual terminal processing
		// For simplicity, we assume Windows 10+ (released 2015)
		return true
	}

	return true
}

// Colorize wraps text with ANSI color codes if colors are enabled.
func Colorize(color, text string) string {
	if !colorsEnabled {
		return text
	}
	return color + text + Reset
}

// Success returns green-colored text for success messages.
func Success(text string) string {
	return Colorize(Green, text)
}

// Error returns red-colored text for error messages.
func Error(text string) string {
	return Colorize(Red, text)
}

// Warning returns yellow-colored text for warning messages.
func Warning(text string) string {
	return Colorize(Yellow, text)
}

// Info returns cyan-colored text for info messages.
func Info(text string) string {
	return Colorize(Cyan, text)
}

// Hint returns dim gray text for hints and secondary information.
func Hint(text string) string {
	if !colorsEnabled {
		return text
	}
	return Dim + Gray + text + Reset
}

// Highlight returns bold text for emphasis.
func Highlight(text string) string {
	if !colorsEnabled {
		return text
	}
	return Bold + text + Reset
}

// ClearScreenIfEnabled clears the screen if colors/TTY are enabled.
func ClearScreenIfEnabled() {
	if colorsEnabled {
		os.Stdout.WriteString(ClearScreen)
	}
}

// IsColorEnabled returns whether colors are currently enabled.
func IsColorEnabled() bool {
	return colorsEnabled
}
