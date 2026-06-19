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
}

// NewProgressTracker creates a new progress tracker.
func NewProgressTracker(total int) *ProgressTracker {
	return &ProgressTracker{
		total:     total,
		startTime: time.Now(),
		frame:     0,
	}
}

// Render draws the progress bar with spinner, percentage, speed, and ETA.
// Uses \r to redraw in place without scrolling.
func (p *ProgressTracker) Render(done int) {
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

	// Calculate speed (wallets/sec)
	speed := 0.0
	if elapsed > 0 {
		speed = float64(done) / elapsed
	}

	// Calculate ETA
	eta := ""
	if speed > 0 && done < p.total {
		remaining := p.total - done
		etaSeconds := float64(remaining) / speed
		eta = formatDuration(time.Duration(etaSeconds * float64(time.Second)))
	} else if done >= p.total {
		eta = "done"
	} else {
		eta = "calculating..."
	}

	// Build progress bar
	filled := int(pct / 100 * barWidth)
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	// Get spinner frame
	spinner := spinnerFrames[p.frame%len(spinnerFrames)]
	p.frame++

	// Render the line (using \r to overwrite)
	fmt.Printf("\r%s [%s] %6.1f%% | %d/%d | %.0f w/s | ETA: %s   ",
		spinner, bar, pct, done, p.total, speed, eta)
}

// Clear clears the progress line.
func (p *ProgressTracker) Clear() {
	fmt.Print("\r\033[K") // Clear line
}

// Finish prints final statistics.
func (p *ProgressTracker) Finish(done int) {
	elapsed := time.Since(p.startTime)
	speed := 0.0
	if elapsed.Seconds() > 0 {
		speed = float64(done) / elapsed.Seconds()
	}

	p.Clear()
	fmt.Printf("✓ Generated %d wallets in %s (%.0f wallets/sec)\n\n",
		done, formatDuration(elapsed), speed)
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
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
