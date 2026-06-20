// Package core — vanity wallet generation engine
package core

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"evmwalletbot/config"
	"evmwalletbot/storage"
	"evmwalletbot/wallet"
)

// VanityConfig holds configuration for vanity address generation
type VanityConfig struct {
	Prefix      string
	Suffix      string
	Checksum    bool
	TargetCount int
}

// VanityStats tracks vanity generation statistics
type VanityStats struct {
	Attempts     atomic.Int64
	MatchesFound atomic.Int64
	StartTime    time.Time
	Speed        atomic.Uint64 // addresses per second (stored as uint64 for atomic ops)
}

// VanityMatch represents a found vanity wallet
type VanityMatch struct {
	Wallet   *wallet.Wallet
	Attempts int64
	Elapsed  time.Duration
}

// CalibrateSpeed measures wallet generation speed for 1 second
func CalibrateSpeed(ctx context.Context, cfg *config.Config) float64 {
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	var attempts atomic.Int64
	calibrationCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-calibrationCtx.Done():
					return
				default:
					w := walletPool.Get().(*wallet.Wallet)
					if err := wallet.GenerateInto(w); err == nil {
						attempts.Add(1)
					}
					walletPool.Put(w)
				}
			}
		}()
	}

	wg.Wait()
	return float64(attempts.Load())
}

// GenerateVanityWallets generates wallets matching the vanity pattern
func GenerateVanityWallets(ctx context.Context, store storage.Storage, cfg *config.Config, vanity VanityConfig) error {
	// Validate patterns
	if err := wallet.ValidateVanityPattern(vanity.Prefix, "prefix"); err != nil {
		return err
	}
	if err := wallet.ValidateVanityPattern(vanity.Suffix, "suffix"); err != nil {
		return err
	}

	// If both prefix and suffix are empty, fall back to normal generation
	if vanity.Prefix == "" && vanity.Suffix == "" {
		log.Println("[INFO] No vanity pattern specified, using normal generation")
		return GenerateWallets(ctx, store, cfg, vanity.TargetCount)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()
	stats := &VanityStats{
		StartTime: start,
	}

	// Calculate difficulty
	difficulty := wallet.CalculateDifficulty(vanity.Prefix, vanity.Suffix, vanity.Checksum)

	// Calibrate speed
	fmt.Print("\n  Calibrating speed... ")
	speed := CalibrateSpeed(ctx, cfg)
	stats.Speed.Store(uint64(speed))
	fmt.Printf("~%.0f addr/s\n", speed)

	// Display pre-flight panel
	showPreFlightPanel(vanity, difficulty, speed)

	// Ask for confirmation if difficulty is high
	if !confirmVanityGeneration(difficulty, speed) {
		return fmt.Errorf("generation cancelled by user")
	}

	// Auto-tune workers
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	log.Printf("[INFO] Starting vanity generation | pattern=%s...%s | checksum=%v | workers=%d\n",
		vanity.Prefix, vanity.Suffix, vanity.Checksum, workers)

	// Channels for matches and progress
	matchCh := make(chan *VanityMatch, 10)
	progressDone := make(chan struct{})

	// Start progress display
	go displayVanityProgress(ctx, stats, vanity.TargetCount, difficulty, progressDone)

	// Start match collector
	matches := make([]*VanityMatch, 0, vanity.TargetCount)
	var matchMu sync.Mutex
	collectorDone := make(chan struct{})

	go func() {
		defer close(collectorDone)
		for match := range matchCh {
			matchMu.Lock()
			matches = append(matches, match)
			matchMu.Unlock()

			// Display match
			displayMatch(match, len(matches), vanity.TargetCount)

			// Check if we've found enough
			if len(matches) >= vanity.TargetCount {
				cancel() // Stop all workers
				return
			}
		}
	}()

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go vanityWorker(ctx, &wg, vanity, stats, matchCh, i)
	}

	// Wait for workers to finish
	wg.Wait()
	close(matchCh)

	// Wait for collector to finish
	<-collectorDone
	close(progressDone)

	// Final progress update
	time.Sleep(150 * time.Millisecond)
	fmt.Println()

	// Save matches to database
	if len(matches) > 0 {
		if err := saveVanityMatches(ctx, store, matches); err != nil {
			return fmt.Errorf("failed to save matches: %w", err)
		}
	}

	// Display summary
	displayVanitySummary(stats, matches, vanity.TargetCount, difficulty)

	return nil
}

// vanityWorker generates wallets and checks for vanity matches
func vanityWorker(ctx context.Context, wg *sync.WaitGroup, vanity VanityConfig, stats *VanityStats, matchCh chan<- *VanityMatch, workerID int) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] Vanity worker %d panic recovered: %v", workerID, r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Generate wallet
			w := walletPool.Get().(*wallet.Wallet)
			if err := wallet.GenerateInto(w); err != nil {
				walletPool.Put(w)
				continue
			}

			attempts := stats.Attempts.Add(1)

			// Check if it matches
			addr := w.AddressHex()
			if wallet.MatchesVanity(addr, vanity.Prefix, vanity.Suffix, vanity.Checksum) {
				// Found a match!
				match := &VanityMatch{
					Wallet:   w,
					Attempts: attempts,
					Elapsed:  time.Since(stats.StartTime),
				}

				select {
				case matchCh <- match:
					// Match sent successfully, don't return wallet to pool
					// It will be saved to database
				case <-ctx.Done():
					walletPool.Put(w)
					return
				}
			} else {
				// No match, return to pool
				walletPool.Put(w)
			}
		}
	}
}

// displayVanityProgress shows live progress with spinner
func displayVanityProgress(ctx context.Context, stats *VanityStats, target int, difficulty float64, done <-chan struct{}) {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	frame := 0
	lastAttempts := int64(0)
	lastTime := time.Now()

	for {
		select {
		case <-ticker.C:
			attempts := stats.Attempts.Load()
			matches := stats.MatchesFound.Load()

			// Calculate current speed
			now := time.Now()
			elapsed := now.Sub(lastTime).Seconds()
			if elapsed > 0 {
				currentSpeed := float64(attempts-lastAttempts) / elapsed
				stats.Speed.Store(uint64(currentSpeed))
				lastAttempts = attempts
				lastTime = now
			}

			speed := float64(stats.Speed.Load())
			prob := wallet.CalculateProbability(attempts, difficulty) * 100

			// Format output
			spinner := spinnerFrames[frame%len(spinnerFrames)]
			fmt.Printf("\r  %s  tried %s  ·  found %d/%d  ·  %s  ·  P≈%.1f%%",
				spinner,
				FormatNumber(int(attempts)),
				matches,
				target,
				wallet.FormatSpeed(speed),
				prob,
			)

			frame++

		case <-done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// displayMatch shows a found vanity wallet
func displayMatch(match *VanityMatch, current, target int) {
	addr := match.Wallet.AddressHex()
	privKey := match.Wallet.PrivateKeyHex()

	// Clear progress line
	fmt.Print("\r" + clearLine())

	fmt.Printf("\n  %s MATCH  %s\n", Success("✓"), Highlight(match.Wallet.ShortAddress()))
	fmt.Printf("    address      %s\n", Info(addr))
	fmt.Printf("    private key  %s\n", Hint("0x"+privKey[:8]+"..."+privKey[len(privKey)-6:]))
	fmt.Printf("    attempts     %s   ·   elapsed %s\n\n",
		FormatNumber(int(match.Attempts)),
		match.Elapsed.Round(time.Millisecond))
}

// showPreFlightPanel displays difficulty and time estimates
func showPreFlightPanel(vanity VanityConfig, difficulty float64, speed float64) {
	time50, time99 := wallet.EstimateTime(difficulty, speed)

	pattern := fmt.Sprintf("0x%s……%s", vanity.Prefix, vanity.Suffix)
	checksumMode := "case-insensitive"
	if vanity.Checksum {
		checksumMode = "case-sensitive"
	}

	fmt.Printf(`
  ╔══════════════════════════════════════════════════════════════╗
  ║                   VANITY GENERATION PREVIEW                  ║
  ╠══════════════════════════════════════════════════════════════╣
  ║  Pattern        : %-43s ║
  ║  Mode           : %-43s ║
  ║  Difficulty     : %-43s ║
  ║  Speed          : %-43s ║
  ║                                                              ║
  ║  Time Estimates:                                             ║
  ║    50%% chance   : %-43s ║
  ║    99%% chance   : %-43s ║
  ║                                                              ║
  ║  Workers        : %-43d ║
  ║  Target matches : %-43d ║
  ╚══════════════════════════════════════════════════════════════╝
`,
		pattern,
		checksumMode,
		wallet.FormatDifficulty(difficulty),
		wallet.FormatSpeed(speed),
		wallet.FormatDuration(time50),
		wallet.FormatDuration(time99),
		runtime.NumCPU(),
		vanity.TargetCount,
	)
}

// confirmVanityGeneration asks user to confirm if difficulty is high
func confirmVanityGeneration(difficulty float64, speed float64) bool {
	time50, _ := wallet.EstimateTime(difficulty, speed)

	// Warn if estimated time is > 10 minutes
	if time50 > 10*time.Minute {
		fmt.Printf("\n  %s This may take a while (estimated: %s for 50%% chance)\n",
			Warning("⚠️"),
			wallet.FormatDuration(time50))
		fmt.Print("  Continue? [y/N]: ")

		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))

		return response == "y" || response == "yes"
	}

	return true
}

// saveVanityMatches saves found vanity wallets to database
func saveVanityMatches(ctx context.Context, store storage.Storage, matches []*VanityMatch) error {
	if len(matches) == 0 {
		return nil
	}

	wallets := make([]*wallet.Wallet, len(matches))
	for i, match := range matches {
		wallets[i] = match.Wallet
	}

	_, err := store.SaveWallets(ctx, wallets)
	if err != nil {
		return fmt.Errorf("database insert failed: %w", err)
	}

	log.Printf("[INFO] Saved %d vanity wallets to database", len(matches))
	return nil
}

// displayVanitySummary shows final statistics
func displayVanitySummary(stats *VanityStats, matches []*VanityMatch, target int, difficulty float64) {
	attempts := stats.Attempts.Load()
	found := int64(len(matches))
	elapsed := time.Since(stats.StartTime)
	avgSpeed := float64(attempts) / elapsed.Seconds()

	fmt.Printf(`
  ╔══════════════════════════════════════════════════════════════╗
  ║                    VANITY GENERATION COMPLETE                ║
  ╠══════════════════════════════════════════════════════════════╣
  ║                                                              ║
  ║  Matches found  : %d / %d%-39s ║
  ║  Total attempts : %-45s ║
  ║  Time elapsed   : %-45s ║
  ║  Average speed  : %-45s ║
  ║  Difficulty     : %-45s ║
  ║                                                              ║
  ╚══════════════════════════════════════════════════════════════╝

`,
		found,
		target,
		"",
		FormatNumber(int(attempts)),
		elapsed.Round(time.Millisecond),
		wallet.FormatSpeed(avgSpeed),
		wallet.FormatDifficulty(difficulty),
	)
}

// clearLine returns ANSI escape code to clear current line
func clearLine() string {
	return "\033[2K"
}
