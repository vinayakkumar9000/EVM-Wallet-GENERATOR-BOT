// Package core — vanity wallet generation engine
package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"evmwalletbot/config"
	"evmwalletbot/storage"
	sqliteStorage "evmwalletbot/storage/sqlite"
	"evmwalletbot/wallet"
)

// VanityConfig holds configuration for vanity address generation
type VanityConfig struct {
	Patterns    []wallet.VanityPattern // Multiple patterns (OR logic - match any)
	Checksum    bool
	TargetCount int

	// Legacy single-pattern support (deprecated, use Patterns instead)
	Prefix string
	Suffix string
}

// VanityStats tracks vanity generation statistics
type VanityStats struct {
	Attempts     atomic.Int64
	MatchesFound atomic.Int64
	StartTime    time.Time
	Speed        atomic.Uint64 // addresses per second (stored as uint64 for atomic ops)
	ResumeID     int64         // ID of resumed search session (0 if new)
}

// VanityMatch represents a found vanity wallet
type VanityMatch struct {
	Wallet       *wallet.Wallet
	Attempts     int64
	Elapsed      time.Duration
	PatternIndex int    // Index of the pattern that matched (-1 for legacy single pattern)
	PatternName  string // Name of the pattern that matched
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
func GenerateVanityWallets(ctx context.Context, _ storage.Storage, cfg *config.Config, vanity VanityConfig) error {
	// Validate patterns
	if len(vanity.Patterns) > 0 {
		for i, p := range vanity.Patterns {
			if err := wallet.ValidateVanityPattern(p.Prefix, fmt.Sprintf("pattern %d prefix", i+1)); err != nil {
				return err
			}
			if err := wallet.ValidateVanityPattern(p.Suffix, fmt.Sprintf("pattern %d suffix", i+1)); err != nil {
				return err
			}
		}
	} else {
		// Legacy single-pattern validation
		if err := wallet.ValidateVanityPattern(vanity.Prefix, "prefix"); err != nil {
			return err
		}
		if err := wallet.ValidateVanityPattern(vanity.Suffix, "suffix"); err != nil {
			return err
		}

		// If both prefix and suffix are empty, return error
		if vanity.Prefix == "" && vanity.Suffix == "" {
			return fmt.Errorf("no vanity pattern specified")
		}
	}

	// Check for existing paused search (only for vanity.db storage)
	vanityStore, err := sqliteStorage.NewVanitySQLiteStorage(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("create vanity storage: %w", err)
	}
	defer vanityStore.Close()

	if err := vanityStore.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate vanity storage: %w", err)
	}

	existingSearch, err := vanityStore.GetActiveVanitySearchState(ctx)
	if err != nil {
		return fmt.Errorf("check for existing search: %w", err)
	}

	var resumeFrom *sqliteStorage.VanitySearchState
	if existingSearch != nil {
		fmt.Printf("\n  %s Found paused search: %d/%d matches, %s attempts\n",
			Info("ℹ"), existingSearch.MatchesFound, existingSearch.TargetCount,
			FormatNumber(int(existingSearch.Attempts)))
		fmt.Print("  Resume this search? [Y/n]: ")

		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))

		if response == "" || response == "y" || response == "yes" {
			resumeFrom = existingSearch
		} else {
			// Mark old search as completed and start fresh
			if err := vanityStore.MarkVanitySearchCompleted(ctx, existingSearch.ID); err != nil {
				log.Printf("[WARN] Failed to mark old search as completed: %v", err)
			}
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup Ctrl+C handler for graceful pause
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	start := time.Now()
	stats := &VanityStats{
		StartTime: start,
		ResumeID:  0,
	}

	// Calculate difficulty (needed for both new and resumed searches)
	var difficulty float64
	if len(vanity.Patterns) > 0 {
		difficulty = wallet.CalculateMultiPatternDifficulty(vanity.Patterns, vanity.Checksum)
	} else {
		difficulty = wallet.CalculateDifficulty(vanity.Prefix, vanity.Suffix, vanity.Checksum)
	}

	// If resuming, restore state
	if resumeFrom != nil {
		stats.Attempts.Store(resumeFrom.Attempts)
		stats.MatchesFound.Store(int64(resumeFrom.MatchesFound))
		stats.StartTime = resumeFrom.StartTime
		stats.ResumeID = resumeFrom.ID
		start = resumeFrom.StartTime

		fmt.Printf("  %s Resuming from %s attempts\n", Success("✓"), FormatNumber(int(resumeFrom.Attempts)))

		// Show difficulty and time estimates for resumed search
		speed := CalibrateSpeed(ctx, cfg)
		stats.Speed.Store(uint64(speed))

		// Calculate remaining difficulty based on attempts so far
		remainingTarget := vanity.TargetCount - resumeFrom.MatchesFound
		if remainingTarget > 0 {
			fmt.Printf("  %s Need %d more matches\n", Info("ℹ"), remainingTarget)
			fmt.Printf("  %s Current speed: ~%.0f addr/s\n", Info("ℹ"), speed)

			// Show time estimates for remaining matches
			time50, time99 := wallet.EstimateTime(difficulty, speed)
			fmt.Printf("  %s Estimated time per match: 50%% chance = %s, 99%% chance = %s\n\n",
				Info("ℹ"), wallet.FormatDuration(time50), wallet.FormatDuration(time99))
		}
	}

	// Calibrate speed (skip if resuming)
	if resumeFrom == nil {
		fmt.Print("\n  Calibrating speed... ")
		speed := CalibrateSpeed(ctx, cfg)
		stats.Speed.Store(uint64(speed))
		fmt.Printf("~%.0f addr/s\n", speed)

		// Display pre-flight panel
		showPreFlightPanel(vanity, difficulty, float64(stats.Speed.Load()))

		// Ask for confirmation if difficulty is high
		if !confirmVanityGeneration(difficulty, float64(stats.Speed.Load())) {
			return fmt.Errorf("generation cancelled by user")
		}
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
			stats.MatchesFound.Store(int64(len(matches)))
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

	// Start periodic state saver (every 5 seconds)
	stateSaverDone := make(chan struct{})
	go func() {
		defer close(stateSaverDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := saveVanitySearchProgress(ctx, vanityStore, stats, vanity, resumeFrom); err != nil {
					log.Printf("[WARN] Failed to save search progress: %v", err)
				}
			case <-ctx.Done():
				// Final save on exit
				if err := saveVanitySearchProgress(ctx, vanityStore, stats, vanity, resumeFrom); err != nil {
					log.Printf("[WARN] Failed to save final search progress: %v", err)
				}
				return
			}
		}
	}()

	// Start Ctrl+C handler for graceful pause
	go func() {
		select {
		case <-sigCh:
			fmt.Printf("\n\n  %s Ctrl+C detected, saving search state...\n", Warning("⚠️"))

			// Cancel context to stop workers
			cancel()

			// Save current state as "paused"
			if stats.ResumeID > 0 {
				// Update existing search
				if err := vanityStore.MarkVanitySearchPaused(context.Background(), stats.ResumeID); err != nil {
					log.Printf("[WARN] Failed to mark search as paused: %v", err)
				}
				if err := vanityStore.UpdateVanitySearchProgress(context.Background(), stats.ResumeID,
					stats.Attempts.Load(), int(stats.MatchesFound.Load())); err != nil {
					log.Printf("[WARN] Failed to save final progress: %v", err)
				}
			} else {
				// Save new search state
				if err := saveVanitySearchProgress(context.Background(), vanityStore, stats, vanity, resumeFrom); err != nil {
					log.Printf("[WARN] Failed to save search state: %v", err)
				}
				if stats.ResumeID > 0 {
					if err := vanityStore.MarkVanitySearchPaused(context.Background(), stats.ResumeID); err != nil {
						log.Printf("[WARN] Failed to mark search as paused: %v", err)
					}
				}
			}

			fmt.Printf("  %s Search paused. Run again to resume from %s attempts.\n\n",
				Success("✓"), FormatNumber(int(stats.Attempts.Load())))
		case <-ctx.Done():
			return
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

	// Save matches to separate vanity database
	if len(matches) > 0 {
		if err := saveVanityMatches(ctx, nil, matches, cfg.DataDir); err != nil {
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
			addr := w.AddressHex()

			// Check if it matches any pattern
			var matched bool
			var patternIndex int
			var patternName string

			if len(vanity.Patterns) > 0 {
				// Multi-pattern mode
				matched, patternIndex, patternName = wallet.MatchesAnyPattern(addr, vanity.Patterns, vanity.Checksum)
			} else {
				// Legacy single-pattern mode
				matched = wallet.MatchesVanity(addr, vanity.Prefix, vanity.Suffix, vanity.Checksum)
				patternIndex = -1
				patternName = fmt.Sprintf("%s...%s", vanity.Prefix, vanity.Suffix)
			}

			if matched {
				// Found a match!
				match := &VanityMatch{
					Wallet:       w,
					Attempts:     attempts,
					Elapsed:      time.Since(stats.StartTime),
					PatternIndex: patternIndex,
					PatternName:  patternName,
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

// displayMatch shows a found vanity wallet (only first 5 matches)
func displayMatch(match *VanityMatch, current, target int) {
	// Only display first 5 matches in terminal
	if current > 5 {
		return
	}

	addr := match.Wallet.AddressHex()
	privKey := match.Wallet.PrivateKeyHex()

	// Clear progress line
	fmt.Print("\r" + clearLine())

	fmt.Printf("\n  %s MATCH %d/%d", Success("✓"), current, target)
	if match.PatternName != "" {
		fmt.Printf(" %s\n", Hint("(pattern: %s)", match.PatternName))
	} else {
		fmt.Println()
	}
	fmt.Printf("    address      %s\n", Info("0x%s", addr))
	fmt.Printf("    private key  %s\n", Hint("0x%s", privKey))
	fmt.Printf("    attempts     %s   ·   elapsed %s\n\n",
		FormatNumber(int(match.Attempts)),
		match.Elapsed.Round(time.Millisecond))

	// Show message after 5th match
	if current == 5 && target > 5 {
		fmt.Printf("  %s (showing first 5 matches only, all %d will be saved to vanity.db)\n\n",
			Hint("..."), target)
	}
}

// showPreFlightPanel displays difficulty and time estimates
func showPreFlightPanel(vanity VanityConfig, difficulty float64, speed float64) {
	time50, time99 := wallet.EstimateTime(difficulty, speed)

	checksumMode := "case-insensitive"
	if vanity.Checksum {
		checksumMode = "case-sensitive (EIP-55)"
	}

	// Format pattern display
	var patternDisplay string
	if len(vanity.Patterns) > 1 {
		patternDisplay = fmt.Sprintf("%d patterns (OR logic)", len(vanity.Patterns))
	} else if len(vanity.Patterns) == 1 {
		p := vanity.Patterns[0]
		patternDisplay = fmt.Sprintf("0x%s……%s", p.Prefix, p.Suffix)
	} else {
		patternDisplay = fmt.Sprintf("0x%s……%s", vanity.Prefix, vanity.Suffix)
	}

	fmt.Printf(`
  ╔══════════════════════════════════════════════════════════════╗
  ║                   VANITY GENERATION PREVIEW                  ║
  ╠══════════════════════════════════════════════════════════════╣
  ║  Pattern(s)     : %-43s ║
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
		patternDisplay,
		checksumMode,
		wallet.FormatDifficulty(difficulty),
		wallet.FormatSpeed(speed),
		wallet.FormatDuration(time50),
		wallet.FormatDuration(time99),
		runtime.NumCPU(),
		vanity.TargetCount,
	)

	// Show individual patterns if multiple
	if len(vanity.Patterns) > 1 {
		fmt.Println("\n  Patterns (match ANY):")
		for i, p := range vanity.Patterns {
			name := p.Name
			if name == "" {
				name = fmt.Sprintf("Pattern %d", i+1)
			}
			fmt.Printf("    %d. %s: 0x%s……%s\n", i+1, name, p.Prefix, p.Suffix)
		}
		fmt.Println()
	}
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

// saveVanityMatches saves found vanity wallets to separate vanity.db
func saveVanityMatches(ctx context.Context, _ storage.Storage, matches []*VanityMatch, dataDir string) error {
	if len(matches) == 0 {
		return nil
	}

	// Create separate vanity storage
	vanityStore, err := sqliteStorage.NewVanitySQLiteStorage(dataDir)
	if err != nil {
		return fmt.Errorf("create vanity storage: %w", err)
	}
	defer vanityStore.Close()

	// Migrate schema
	if err := vanityStore.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate vanity storage: %w", err)
	}

	wallets := make([]*wallet.Wallet, len(matches))
	for i, match := range matches {
		wallets[i] = match.Wallet
	}

	_, err = vanityStore.SaveWallets(ctx, wallets)
	if err != nil {
		return fmt.Errorf("database insert failed: %w", err)
	}

	log.Printf("[INFO] Saved %d vanity wallets to vanity.db", len(matches))
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
  ║  All %d matches saved to vanity.db%-27s ║
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
		found,
		"",
	)
}

// clearLine returns ANSI escape code to clear current line
func clearLine() string {
	return "\033[2K"
}

// saveVanitySearchProgress saves current search progress to database
func saveVanitySearchProgress(ctx context.Context, store *sqliteStorage.SQLiteStorage, stats *VanityStats, vanity VanityConfig, resumeFrom *sqliteStorage.VanitySearchState) error {
	attempts := stats.Attempts.Load()
	matchesFound := int(stats.MatchesFound.Load())

	// Serialize patterns
	var patternsJSON string
	var err error
	if len(vanity.Patterns) > 0 {
		patternsJSON, err = sqliteStorage.SerializePatterns(vanity.Patterns)
	} else {
		// Legacy single pattern
		legacyPattern := []wallet.VanityPattern{{
			Prefix: vanity.Prefix,
			Suffix: vanity.Suffix,
			Name:   "Pattern 1",
		}}
		patternsJSON, err = sqliteStorage.SerializePatterns(legacyPattern)
	}
	if err != nil {
		return fmt.Errorf("serialize patterns: %w", err)
	}

	if resumeFrom != nil {
		// Update existing search
		return store.UpdateVanitySearchProgress(ctx, resumeFrom.ID, attempts, matchesFound)
	}

	// Create new search state
	state := &sqliteStorage.VanitySearchState{
		Patterns:     patternsJSON,
		Checksum:     vanity.Checksum,
		TargetCount:  vanity.TargetCount,
		Attempts:     attempts,
		MatchesFound: matchesFound,
		StartTime:    stats.StartTime,
		LastUpdate:   time.Now(),
		Status:       "active",
	}

	if err := store.SaveVanitySearchState(ctx, state); err != nil {
		return fmt.Errorf("save search state: %w", err)
	}

	// Update stats with new ID
	stats.ResumeID = state.ID
	return nil
}
