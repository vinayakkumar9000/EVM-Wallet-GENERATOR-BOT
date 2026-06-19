// Package core — live progress display with spinner, bar, speed, and ETA.
package core

import (
	"fmt"
	"strings"
	"time"
)

// Braille spinner frames for smooth animation
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
		formatNumber(done), formatNumber(p.total))

	// Line 2: Speed metrics
	fmt.Printf("\r\033[K       speed    %s /s          peak     %s /s\n",
		formatNumber(int(speed)), formatNumber(int(p.peakRate)))

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

	fmt.Printf("\n%s\n", Success("✓ Generation complete"))
	fmt.Printf("  %s wallets in %s\n",
		formatNumber(done),
		formatDuration(elapsed))
	fmt.Printf("  Average: %s /s  |  Peak: %s /s\n\n",
		formatNumber(int(avgRate)),
		formatNumber(int(p.peakRate)))
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

// formatNumber formats an integer with thousand separators.
func formatNumber(n int) string {
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
