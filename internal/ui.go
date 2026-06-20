package internal

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"
	"golang.org/x/text/width"
)

// ============================================================================
// Constants
// ============================================================================

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

// Braille spinner frames for smooth animation
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var (
	// colorsEnabled is set once at init based on TTY detection and NO_COLOR
	colorsEnabled bool
	ansiEscape    = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
)

func init() {
	// Check if colors should be enabled
	colorsEnabled = shouldEnableColors()
}

// ============================================================================
// Color Detection
// ============================================================================

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

// IsColorEnabled returns whether colors are currently enabled.
func IsColorEnabled() bool {
	return colorsEnabled
}

// ============================================================================
// Color Functions
// ============================================================================

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

// ============================================================================
// Progress Display
// ============================================================================

// ProgressTracker tracks generation progress and calculates metrics.
type ProgressTracker struct {
	total     int
	startTime time.Time
	frame     int
	peakRate  float64
	lastCount int
	lastTime  time.Time
}

// NewProgressTracker creates a new progress tracker.
func NewProgressTracker(total int) *ProgressTracker {
	now := time.Now()
	return &ProgressTracker{
		total:     total,
		startTime: now,
		lastTime:  now,
		frame:     0,
		peakRate:  0,
		lastCount: 0,
	}
}

// Render draws the progress bar with spinner, percentage, speed, and ETA.
// Uses multi-line display with cursor positioning for smooth updates.
func (p *ProgressTracker) Render(done int) {
	if !IsColorEnabled() {
		// Fallback: print every 10% milestone
		pct := float64(done) / float64(p.total) * 100
		if int(pct)%10 == 0 && done > p.lastCount {
			fmt.Printf("Progress: %d/%d (%.0f%%)\n", done, p.total, pct)
			p.lastCount = done
		}
		return
	}

	const barWidth = 40

	// Calculate metrics
	elapsed := time.Since(p.startTime).Seconds()
	pct := 0.0
	if p.total > 0 {
		pct = float64(done) / float64(p.total) * 100
		if pct > 100 {
			pct = 100
		}
	}

	// Calculate current speed (wallets/sec)
	speed := 0.0
	if elapsed > 0 {
		speed = float64(done) / elapsed
	}

	// Track peak rate
	if speed > p.peakRate {
		p.peakRate = speed
	}

	// Calculate ETA
	eta := 0.0
	if speed > 0 && done < p.total {
		remaining := p.total - done
		eta = float64(remaining) / speed
	}

	// Build progress bar
	frac := pct / 100
	filled := int(frac * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	// Get spinner frame
	spinner := spinnerFrames[p.frame%len(spinnerFrames)]
	p.frame++

	// Render multi-line display (3 lines)
	// Line 1: Spinner, bar, percentage, count
	fmt.Printf("\r\033[K   %s  %s  %3.0f%%   %s / %s\n",
		spinner, bar, pct,
		FormatNumber(done), FormatNumber(p.total))

	// Line 2: Speed metrics
	fmt.Printf("\r\033[K       speed    %s /s          peak     %s /s\n",
		FormatNumber(int(speed)), FormatNumber(int(p.peakRate)))

	// Line 3: Time metrics
	fmt.Printf("\r\033[K       elapsed  %.2fs              eta      %.2fs",
		elapsed, eta)

	// Move cursor back up 2 lines for next update
	fmt.Print("\033[2A")

	p.lastCount = done
	p.lastTime = time.Now()
}

// Clear clears the progress display (3 lines).
func (p *ProgressTracker) Clear() {
	if !IsColorEnabled() {
		return
	}
	// Move down 2 lines, clear all 3 lines
	fmt.Print("\033[2B")
	fmt.Print("\r\033[K\033[1A\r\033[K\033[1A\r\033[K")
}

// Finish prints final statistics with enhanced formatting.
func (p *ProgressTracker) Finish(done int) {
	// Move cursor down to clear progress area
	if IsColorEnabled() {
		fmt.Print("\033[2B\r\033[K\n")
	}

	elapsed := time.Since(p.startTime)
	avgRate := 0.0
	if elapsed.Seconds() > 0 {
		avgRate = float64(done) / elapsed.Seconds()
	}
	if done > 0 && avgRate > p.peakRate {
		p.peakRate = avgRate
	}

	fmt.Printf("\n%s\n", Success("✓ Generation complete"))
	fmt.Printf("  %s wallets in %s\n",
		FormatNumber(done),
		formatDuration(elapsed))
	fmt.Printf("  Average: %s /s  |  Peak: %s /s\n\n",
		FormatNumber(int(avgRate)),
		FormatNumber(int(p.peakRate)))
}

// ============================================================================
// Formatting Utilities
// ============================================================================

// FormatNumber formats an integer with thousand separators.
func FormatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// FormatBytes renders a byte count in human-readable form.
func FormatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// ============================================================================
// Box Drawing Utilities
// ============================================================================

// StripANSI removes ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// DisplayWidth returns the terminal display width of s (ANSI codes ignored).
func DisplayWidth(s string) int {
	plain := StripANSI(s)
	w := 0
	for _, r := range plain {
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth, width.EastAsianAmbiguous:
			w += 2
		default:
			w += 1
		}
	}
	return w
}

// BoxTop prints the top border of a box with the given inner width.
func BoxTop(innerWidth int) string {
	return "  ╔" + strings.Repeat("═", innerWidth) + "╗"
}

// BoxBottom prints the bottom border of a box with the given inner width.
func BoxBottom(innerWidth int) string {
	return "  ╚" + strings.Repeat("═", innerWidth) + "╝"
}

// BoxRow formats a box row with content aligned inside innerWidth columns.
// align: -1 left, 0 center, 1 right.
func BoxRow(innerWidth int, content string, align int) string {
	dw := DisplayWidth(content)
	pad := innerWidth - dw
	if pad < 0 {
		pad = 0
	}

	var left, right int
	switch {
	case align < 0:
		left, right = 0, pad
	case align > 0:
		left, right = pad, 0
	default:
		left = pad / 2
		right = pad - left
	}

	return fmt.Sprintf("  ║%s%s%s║", strings.Repeat(" ", left), content, strings.Repeat(" ", right))
}

// clearLine returns ANSI escape code to clear current line
func clearLine() string {
	return "\033[2K"
}

// printRow prints a formatted row for statistics display
func printRow(label, value string) {
	fmt.Printf("  ║  %-26s : %-20s ║\n", label, value)
}
