// Package core — ANSI color utilities with TTY detection and NO_COLOR support.
package core

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/term"
)

// ANSI color codes
const (
	Reset = "\033[0m"
	Bold  = "\033[1m"
	Dim   = "\033[2m"

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
// Supports printf-style formatting when additional arguments are provided.
func Success(format string, a ...any) string {
	if len(a) == 0 {
		return Colorize(Green, format)
	}
	return Colorize(Green, fmt.Sprintf(format, a...))
}

// Error returns red-colored text for error messages.
// Supports printf-style formatting when additional arguments are provided.
func Error(format string, a ...any) string {
	if len(a) == 0 {
		return Colorize(Red, format)
	}
	return Colorize(Red, fmt.Sprintf(format, a...))
}

// Warning returns yellow-colored text for warning messages.
// Supports printf-style formatting when additional arguments are provided.
func Warning(format string, a ...any) string {
	if len(a) == 0 {
		return Colorize(Yellow, format)
	}
	return Colorize(Yellow, fmt.Sprintf(format, a...))
}

// Info returns cyan-colored text for info messages.
// Supports printf-style formatting when additional arguments are provided.
func Info(format string, a ...any) string {
	if len(a) == 0 {
		return Colorize(Cyan, format)
	}
	return Colorize(Cyan, fmt.Sprintf(format, a...))
}

// Hint returns dim gray text for hints and secondary information.
// Supports printf-style formatting when additional arguments are provided.
func Hint(format string, a ...any) string {
	text := format
	if len(a) > 0 {
		text = fmt.Sprintf(format, a...)
	}
	if !colorsEnabled {
		return text
	}
	return Dim + Gray + text + Reset
}

// Highlight returns bold text for emphasis.
// Supports printf-style formatting when additional arguments are provided.
func Highlight(format string, a ...any) string {
	text := format
	if len(a) > 0 {
		text = fmt.Sprintf(format, a...)
	}
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
