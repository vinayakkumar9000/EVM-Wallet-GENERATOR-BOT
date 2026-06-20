package internal

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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================================
// Constants
// ============================================================================

const (
	// Progress update interval for terminal display
	ProgressUpdateInterval = 200 * time.Millisecond

	// Retry configuration
	RetryInitialDelay = 100 * time.Millisecond
	RetryMaxDelay     = 5 * time.Second

	// Batch processing delays
	BatchProcessDelay = 50 * time.Millisecond

	// Health check timeout
	HealthCheckTimeout = 5 * time.Minute

	// Connection pool monitoring interval
	PoolMonitorInterval = 30 * time.Second

	// Graceful shutdown grace period
	ShutdownGracePeriod = 2 * time.Second

	// Pool warmup configuration
	MinPoolWarmup    = 100 // Minimum objects to pre-allocate
	MaxPoolWarmup    = 256 // Maximum objects to pre-allocate
	WarmupMultiplier = 16  // Objects per CPU core
)

// ============================================================================
// Wallet Pool
// ============================================================================

// walletPool reuses wallet objects to reduce GC pressure.
// ponytail: sync.Pool is stdlib, no new dependency needed.
var walletPool = sync.Pool{
	New: func() interface{} {
		return &Wallet{
			Address:    make([]byte, 20),
			PrivateKey: make([]byte, 32),
		}
	},
}

// init pre-warms the wallet pool with objects to reduce initial allocation spike.
// ponytail: Dynamic warmup based on CPU cores.
// Ceiling: MaxPoolWarmup objects. Upgrade: make configurable if needed.
func init() {
	warmupSize := runtime.NumCPU() * WarmupMultiplier
	if warmupSize > MaxPoolWarmup {
		warmupSize = MaxPoolWarmup
	}
	if warmupSize < MinPoolWarmup {
		warmupSize = MinPoolWarmup
	}

	for i := 0; i < warmupSize; i++ {
		walletPool.Put(&Wallet{
			Address:    make([]byte, 20),
			PrivateKey: make([]byte, 32),
		})
	}
}

// ============================================================================
// Wallet Generation Engine
// ============================================================================

// GenerateWallets generates `totalWallets` EVM wallets in parallel, inserts them
// into the storage backend, and updates a single terminal line in-place.
func GenerateWallets(ctx context.Context, store Storage, cfg *Config, totalWallets int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()

	// ponytail: Auto-tune workers based on CPU cores (stdlib runtime package).
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > totalWallets {
		workers = totalWallets
	}

	// Initialize exporter if enabled
	var exporter *Exporter
	var exportErr error
	if cfg.ExportEnabled {
		exporter, exportErr = NewExporter(*cfg)
		if exportErr != nil {
			log.Printf("[WARN] Failed to initialize exporter: %v (continuing without export)", exportErr)
		} else {
			defer func() {
				if err := exporter.Close(); err != nil {
					log.Printf("[ERROR] Failed to close exporter: %v", err)
				}
			}()
			log.Printf("[INFO] Export enabled: mode=%s dir=%s", cfg.ExportMode, cfg.ExportDir)
		}
	}

	log.Printf("[INFO] Generating %d wallets | workers=%d (auto-tuned) | DB chunk=%d\n",
		totalWallets, workers, cfg.BatchSize)

	// ── Progress tracking ─────────────────────────────────────────────────
	var confirmedCount atomic.Int64
	progressDone := make(chan struct{})

	fmt.Printf("\n")
	tracker := NewProgressTracker(totalWallets)

	// Start progress rendering goroutine
	go func() {
		ticker := time.NewTicker(120 * time.Millisecond) // ~8 FPS for smooth animation
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tracker.Render(int(confirmedCount.Load()))
			case <-progressDone:
				tracker.Render(int(confirmedCount.Load()))
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// ── Batch event tracking (simplified from per-wallet events) ──────────
	// ponytail: Batch-level logging instead of per-wallet events
	var batchesCompleted atomic.Int64

	// ── Parallel key-generation goroutines with backpressure ──────────────
	// ponytail: Use bounded channel to prevent memory explosion
	// Worker pool + buffered channel provides natural backpressure
	walletCh := make(chan *Wallet, cfg.BatchSize*2)

	var genWG sync.WaitGroup
	perWorker := totalWallets / workers
	remainder := totalWallets % workers

	for i := 0; i < workers; i++ {
		count := perWorker
		if i < remainder {
			count++
		}
		genWG.Add(1)
		go func(n int, workerID int) {
			defer genWG.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[ERROR] Generator worker %d panic recovered: %v", workerID, r)
				}
			}()

			for j := 0; j < n; j++ {
				// Check context cancellation
				select {
				case <-ctx.Done():
					log.Printf("[INFO] Worker %d stopping due to context cancellation", workerID)
					return
				default:
				}

				// ponytail: Reuse wallet objects from pool to reduce GC pressure
				w := walletPool.Get().(*Wallet)
				if err := GenerateInto(w); err != nil {
					log.Printf("[WARN] Key generation error (skipping): %v", err)
					// DO NOT return corrupted object to pool
					continue
				}

				// Send with context cancellation check to prevent blocking during shutdown
				select {
				case walletCh <- w:
					// Successfully sent
				case <-ctx.Done():
					log.Printf("[INFO] Worker %d stopping during send (context cancelled)", workerID)
					return
				}
			}
		}(count, i)
	}

	go func() {
		genWG.Wait()
		close(walletCh)
	}()

	// ── Sequential batch inserter using COPY ──────────────────────────────
	batch := make([]*Wallet, 0, cfg.BatchSize)
	batchNum := 0

	for w := range walletCh {
		batch = append(batch, w)

		if len(batch) >= cfg.BatchSize {
			batchNum++

			// Retry storage insert with exponential backoff
			var ids []int64
			retryErr := WithRetry(ctx, DefaultRetryConfig(), func() error {
				var err error
				ids, err = store.SaveWallets(ctx, batch)
				return err
			})

			if retryErr != nil {
				close(progressDone)
				cancel()     // Cancel context to stop workers
				genWG.Wait() // Wait for all workers to finish
				return fmt.Errorf("storage insert (batch %d) failed after retries: %w", batchNum, retryErr)
			}
			confirmedCount.Add(int64(len(ids)))
			batchesCompleted.Add(1)

			// Export wallets if exporter is enabled
			if exporter != nil {
				for _, w := range batch {
					if err := exporter.Export(w.AddressHex(), "0x"+w.PrivateKeyHex()); err != nil {
						log.Printf("[WARN] Export failed for wallet: %v", err)
					}
				}
			}

			// Log batch completion (not per-wallet) - optional via config
			if cfg.EnableLogging {
				log.Printf("[INFO] Batch %d complete: %d wallets inserted", batchNum, len(ids))
			}

			// ponytail: Return wallet objects to pool for reuse.
			// Reset wallet data before returning to pool to prevent data leakage
			for _, w := range batch {
				// Clear sensitive data
				for i := range w.Address {
					w.Address[i] = 0
				}
				for i := range w.PrivateKey {
					w.PrivateKey[i] = 0
				}
				walletPool.Put(w)
			}
			batch = batch[:0]
		}
	}

	// ── Flush remainder ───────────────────────────────────────────────────
	if len(batch) > 0 {
		batchNum++

		// Retry storage insert with exponential backoff
		var ids []int64
		retryErr := WithRetry(ctx, DefaultRetryConfig(), func() error {
			var err error
			ids, err = store.SaveWallets(ctx, batch)
			return err
		})

		if retryErr != nil {
			close(progressDone)
			cancel()     // Cancel context to stop workers
			genWG.Wait() // Wait for all workers to finish
			return fmt.Errorf("storage insert (final batch) failed after retries: %w", retryErr)
		}
		confirmedCount.Add(int64(len(ids)))
		batchesCompleted.Add(1)

		// Export wallets if exporter is enabled
		if exporter != nil {
			for _, w := range batch {
				if err := exporter.Export(w.AddressHex(), "0x"+w.PrivateKeyHex()); err != nil {
					log.Printf("[WARN] Export failed for wallet: %v", err)
				}
			}
		}

		// Log batch completion (not per-wallet) - optional via config
		if cfg.EnableLogging {
			log.Printf("[INFO] Final batch %d complete: %d wallets inserted", batchNum, len(ids))
		}

		// Reset wallet data before returning to pool to prevent data leakage
		for _, w := range batch {
			// Clear sensitive data
			for i := range w.Address {
				w.Address[i] = 0
			}
			for i := range w.PrivateKey {
				w.PrivateKey[i] = 0
			}
			walletPool.Put(w)
		}
	}

	close(progressDone)
	time.Sleep(150 * time.Millisecond) // Let final render complete

	done := int(confirmedCount.Load())
	elapsed := time.Since(start)
	tracker.Finish(done)

	// Flush exporter and get export count
	var exportCount int
	if exporter != nil {
		if err := exporter.Flush(); err != nil {
			log.Printf("[WARN] Failed to flush exporter: %v", err)
		}
		exportCount = exporter.Count()
	}

	log.Printf("[INFO] Generation complete: %d wallets in %v (%.2f wallets/sec)",
		done, elapsed, float64(done)/elapsed.Seconds())

	if exportCount > 0 {
		log.Printf("[INFO] Export complete: %d wallets exported to %s (mode: %s)",
			exportCount, cfg.ExportDir, cfg.ExportMode)
	}

	return nil
}

// ============================================================================
// Benchmark
// ============================================================================

// BenchmarkWalletGeneration generates wallets for benchmarking WITHOUT storing them.
// This is used purely for performance measurement and tuning.
func BenchmarkWalletGeneration(ctx context.Context, cfg *Config, totalWallets int) (time.Duration, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()

	// Auto-tune workers based on CPU cores
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > totalWallets {
		workers = totalWallets
	}

	// Counter for generated wallets
	var generated atomic.Int64

	// Worker pool for parallel generation
	var wg sync.WaitGroup
	perWorker := totalWallets / workers
	remainder := totalWallets % workers

	for i := 0; i < workers; i++ {
		count := perWorker
		if i < remainder {
			count++
		}
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < n; j++ {
				// Check context cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Generate wallet (throwaway, not stored)
				w := walletPool.Get().(*Wallet)
				if err := GenerateInto(w); err != nil {
					walletPool.Put(w)
					continue
				}

				// Immediately return to pool - no storage
				generated.Add(1)
				walletPool.Put(w)
			}
		}(count)
	}

	// Wait for all workers to finish
	wg.Wait()

	elapsed := time.Since(start)
	return elapsed, nil
}

// ============================================================================
// Statistics
// ============================================================================

// GetStats queries statistics from the active storage backend.
func GetStats(ctx context.Context, store Storage) (*Stats, error) {
	return store.GetStats(ctx)
}

// PrintStats renders the statistics table to stdout.
func PrintStats(s *Stats) {
	line := "  ├─────────────────────────────────────────────────┤"
	top := "  ╔═════════════════════════════════════════════════╗"
	bot := "  ╚═════════════════════════════════════════════════╝"
	title := "  ║              WALLET STATISTICS                 ║"

	fmt.Println()
	fmt.Println(top)
	fmt.Println(title)
	fmt.Println(line)
	printRow("Total wallets", fmt.Sprintf("%d", s.TotalWallets))
	printRow("Wallets created today", fmt.Sprintf("%d", s.WalletsToday))
	printRow("Unused wallets", fmt.Sprintf("%d", s.UnusedWallets))
	printRow("Used wallets", fmt.Sprintf("%d", s.UsedWallets))
	fmt.Println(line)
	printRow("Total events logged", fmt.Sprintf("%d", s.TotalEvents))
	printRow("Database size", FormatBytes(s.DBSizeBytes))
	fmt.Println(line)

	if !s.NewestWallet.IsZero() {
		printRow("Last wallet created", s.NewestWallet.Format("2006-01-02 15:04:05"))
	} else {
		printRow("Last wallet created", "N/A — no wallets yet")
	}

	fmt.Println(bot)
	fmt.Println()
}

// ============================================================================
// Vanity Address Generation
// ============================================================================

// VanityConfig holds configuration for vanity address generation
type VanityConfig struct {
	Patterns    []VanityPattern // Multiple patterns (OR logic - match any)
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
	Wallet       *Wallet
	Attempts     int64
	Elapsed      time.Duration
	PatternIndex int    // Index of the pattern that matched (-1 for legacy single pattern)
	PatternName  string // Name of the pattern that matched
}

// CalibrateSpeed measures wallet generation speed for 1 second
func CalibrateSpeed(ctx context.Context, cfg *Config) float64 {
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
					w := walletPool.Get().(*Wallet)
					if err := GenerateInto(w); err == nil {
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
func GenerateVanityWallets(ctx context.Context, _ Storage, cfg *Config, vanity VanityConfig) error {
	// Validate patterns
	if len(vanity.Patterns) > 0 {
		for i, p := range vanity.Patterns {
			if err := ValidateVanityPattern(p.Prefix, fmt.Sprintf("pattern %d prefix", i+1)); err != nil {
				return err
			}
			if err := ValidateVanityPattern(p.Suffix, fmt.Sprintf("pattern %d suffix", i+1)); err != nil {
				return err
			}
		}
	} else {
		// Legacy single-pattern validation
		if err := ValidateVanityPattern(vanity.Prefix, "prefix"); err != nil {
			return err
		}
		if err := ValidateVanityPattern(vanity.Suffix, "suffix"); err != nil {
			return err
		}

		// If both prefix and suffix are empty, return error
		if vanity.Prefix == "" && vanity.Suffix == "" {
			return fmt.Errorf("no vanity pattern specified")
		}
	}

	// Check for existing paused search (only for vanity.db storage)
	vanityStore, err := NewVanitySQLiteStorage(cfg.DataDir)
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

	var resumeFrom *VanitySearchState
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
		difficulty = CalculateMultiPatternDifficulty(vanity.Patterns, vanity.Checksum)
	} else {
		difficulty = CalculateDifficulty(vanity.Prefix, vanity.Suffix, vanity.Checksum)
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
			time50, time99 := EstimateTime(difficulty, speed)
			fmt.Printf("  %s Estimated time per match: 50%% chance = %s, 99%% chance = %s\n\n",
				Info("ℹ"), FormatDuration(time50), FormatDuration(time99))
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
				// Final save on exit with fresh context (original may be cancelled)
				saveCtx := context.Background()
				if err := saveVanitySearchProgress(saveCtx, vanityStore, stats, vanity, resumeFrom); err != nil {
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

	// Save matches to separate vanity database using fresh context
	// (original ctx may be cancelled after generation completes)
	if len(matches) > 0 {
		saveCtx := context.Background()
		if err := saveVanityMatches(saveCtx, nil, matches, cfg.DataDir); err != nil {
			log.Printf("[WARN] Failed to save matches to vanity.db: %v", err)
			fmt.Printf("  %s Failed to save to vanity.db, but generation completed successfully\n", Warning("⚠"))
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
			w := walletPool.Get().(*Wallet)
			if err := GenerateInto(w); err != nil {
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
				matched, patternIndex, patternName = MatchesAnyPattern(addr, vanity.Patterns, vanity.Checksum)
			} else {
				// Legacy single-pattern mode
				matched = MatchesVanity(addr, vanity.Prefix, vanity.Suffix, vanity.Checksum)
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
			prob := CalculateProbability(attempts, difficulty) * 100

			// Format output
			spinner := spinnerFrames[frame%len(spinnerFrames)]
			fmt.Printf("\r  %s  tried %s  ·  found %d/%d  ·  %s  ·  P≈%.1f%%",
				spinner,
				FormatNumber(int(attempts)),
				matches,
				target,
				FormatSpeed(speed),
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
	addr := match.Wallet.AddressHex()
	privKey := match.Wallet.PrivateKeyHex()

	// Clear progress line
	fmt.Print("\r" + clearLine())

	// Only display first 5 matches in terminal
	if current <= 5 {
		fmt.Printf("\n  ╔═══════════════════════════════════════════════════════════════╗\n")
		fmt.Printf("  ║ %s MATCH %d/%d%-47s║\n", Success("✓"), current, target, "")
		if match.PatternName != "" {
			patternInfo := fmt.Sprintf("Pattern: %s", match.PatternName)
			padding := 61 - len(patternInfo)
			if padding < 0 {
				padding = 0
			}
			fmt.Printf("  ║ %s%s║\n", Hint(patternInfo), strings.Repeat(" ", padding))
		}
		fmt.Printf("  ╟───────────────────────────────────────────────────────────────╢\n")
		fmt.Printf("  ║ Address:     %-48s║\n", Info(addr))
		fmt.Printf("  ║ Private Key: %-48s║\n", Hint(privKey))
		fmt.Printf("  ╟───────────────────────────────────────────────────────────────╢\n")
		fmt.Printf("  ║ Attempts: %-15s  Elapsed: %-23s║\n",
			FormatNumber(int(match.Attempts)),
			match.Elapsed.Round(time.Millisecond).String())
		fmt.Printf("  ╚═══════════════════════════════════════════════════════════════╝\n\n")

		// Show message after 5th match
		if current == 5 && target > 5 {
			fmt.Printf("  %s Showing first 5 matches only. All %d wallets will be saved to vanity.db\n\n",
				Info("ℹ"), target)
		}
	} else if current == 6 {
		// Show progress indicator for remaining matches
		fmt.Printf("  %s Finding remaining matches... (%d/%d found)\n", 
			Info("ℹ"), current, target)
	}
}

// showPreFlightPanel displays difficulty and time estimates
func showPreFlightPanel(vanity VanityConfig, difficulty float64, speed float64) {
	time50, time99 := EstimateTime(difficulty, speed)

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
		FormatDifficulty(difficulty),
		FormatSpeed(speed),
		FormatDuration(time50),
		FormatDuration(time99),
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
	time50, _ := EstimateTime(difficulty, speed)

	// Warn if estimated time is > 10 minutes
	if time50 > 10*time.Minute {
		fmt.Printf("\n  %s This may take a while (estimated: %s for 50%% chance)\n",
			Warning("⚠️"),
			FormatDuration(time50))
		fmt.Print("  Continue? [y/N]: ")

		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))

		return response == "y" || response == "yes"
	}

	return true
}

// saveVanityMatches saves found vanity wallets to separate vanity.db
func saveVanityMatches(ctx context.Context, _ Storage, matches []*VanityMatch, dataDir string) error {
	if len(matches) == 0 {
		return nil
	}

	// Create separate vanity storage
	vanityStore, err := NewVanitySQLiteStorage(dataDir)
	if err != nil {
		return fmt.Errorf("create vanity storage: %w", err)
	}
	defer vanityStore.Close()

	// Migrate schema
	if err := vanityStore.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate vanity storage: %w", err)
	}

	wallets := make([]*Wallet, len(matches))
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

	fmt.Println()
	fmt.Println(Highlight("  ═══════════════════════════════════════════════════════════════"))
	fmt.Println(Success("  ✓ VANITY GENERATION COMPLETE"))
	fmt.Println(Highlight("  ═══════════════════════════════════════════════════════════════"))
	fmt.Println()
	fmt.Printf("  %s Matches found:  %d / %d\n", Info("ℹ"), found, target)
	fmt.Printf("  %s Total attempts: %s\n", Info("ℹ"), FormatNumber(int(attempts)))
	fmt.Printf("  %s Time elapsed:   %s\n", Info("ℹ"), elapsed.Round(time.Millisecond))
	fmt.Printf("  %s Average speed:  %s\n", Info("ℹ"), FormatSpeed(avgSpeed))
	fmt.Printf("  %s Difficulty:     %s\n", Info("ℹ"), FormatDifficulty(difficulty))
	fmt.Println()
	fmt.Println(Highlight("  ───────────────────────────────────────────────────────────────"))
	fmt.Printf("  %s All %d wallet(s) saved to vanity.db\n", Success("✓"), found)
	fmt.Println(Highlight("  ═══════════════════════════════════════════════════════════════"))
	fmt.Println()
}



// saveVanitySearchProgress saves current search progress to database
func saveVanitySearchProgress(ctx context.Context, store *SQLiteStorage, stats *VanityStats, vanity VanityConfig, resumeFrom *VanitySearchState) error {
	attempts := stats.Attempts.Load()
	matchesFound := int(stats.MatchesFound.Load())

	// Serialize patterns
	var patternsJSON string
	var err error
	if len(vanity.Patterns) > 0 {
		patternsJSON, err = SerializePatterns(vanity.Patterns)
	} else {
		// Legacy single pattern
		legacyPattern := []VanityPattern{{
			Prefix: vanity.Prefix,
			Suffix: vanity.Suffix,
			Name:   "Pattern 1",
		}}
		patternsJSON, err = SerializePatterns(legacyPattern)
	}
	if err != nil {
		return fmt.Errorf("serialize patterns: %w", err)
	}

	if resumeFrom != nil {
		// Update existing search
		return store.UpdateVanitySearchProgress(ctx, resumeFrom.ID, attempts, matchesFound)
	}

	// Create new search state
	state := &VanitySearchState{
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

// ============================================================================
// Database Health Monitoring
// ============================================================================

// HealthMetrics represents health statistics for a database table.
type HealthMetrics struct {
	TableName      string
	TotalSize      int64
	IndexSize      int64
	LiveTuples     int64
	DeadTuples     int64
	LastVacuum     *time.Time
	LastAutovacuum *time.Time
	BloatPercent   float64
}

// RunHealthCheck collects metrics, displays them, and records to database when supported.
func RunHealthCheck(ctx context.Context, store Storage) error {
	if err := store.HealthCheck(ctx); err != nil {
		return fmt.Errorf("storage health check: %w", err)
	}

	pgStore, ok := store.(*PostgresStorage)
	if !ok {
		stats, err := store.GetStats(ctx)
		if err != nil {
			return fmt.Errorf("load stats: %w", err)
		}
		fmt.Printf(`
  ┌──────────────────────────────────────────────────────┐
  │                STORAGE HEALTH                        │
  ├──────────────────────────────────────────────────────┤
  │  Backend          : %-33s │
  │  Status           : %-33s │
  │  Total wallets    : %-33d │
  │  Storage size     : %-33s │
  └──────────────────────────────────────────────────────┘
`,
			store.StorageType(),
			"healthy",
			stats.TotalWallets,
			FormatBytes(stats.DBSizeBytes),
		)
		log.Println("[INFO] Health check complete")
		return nil
	}

	log.Println("[INFO] Collecting database health metrics...")
	metrics, err := CollectHealthMetrics(ctx, pgStore.Pool())
	if err != nil {
		return fmt.Errorf("collect metrics: %w", err)
	}

	PrintHealthMetrics(metrics)

	log.Println("[INFO] Recording metrics to database_health table...")
	if err := RecordHealthMetrics(ctx, pgStore.Pool(), metrics); err != nil {
		return fmt.Errorf("record metrics: %w", err)
	}

	log.Println("[INFO] Health check complete")
	return nil
}

// CollectHealthMetrics queries PostgreSQL system catalogs for table health data.
func CollectHealthMetrics(ctx context.Context, pool *pgxpool.Pool) ([]HealthMetrics, error) {
	query := `
		SELECT 
			schemaname || '.' || relname AS table_name,
			pg_total_relation_size(relid) AS total_size,
			pg_indexes_size(relid) AS index_size,
			n_live_tup AS live_tuples,
			n_dead_tup AS dead_tuples,
			last_vacuum,
			last_autovacuum
		FROM pg_stat_user_tables
		WHERE schemaname = 'public'
		ORDER BY pg_total_relation_size(relid) DESC
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query health metrics: %w", err)
	}
	defer rows.Close()

	var metrics []HealthMetrics
	for rows.Next() {
		var m HealthMetrics
		err := rows.Scan(
			&m.TableName,
			&m.TotalSize,
			&m.IndexSize,
			&m.LiveTuples,
			&m.DeadTuples,
			&m.LastVacuum,
			&m.LastAutovacuum,
		)
		if err != nil {
			return nil, fmt.Errorf("scan health metrics: %w", err)
		}

		if m.LiveTuples > 0 {
			m.BloatPercent = float64(m.DeadTuples) / float64(m.LiveTuples+m.DeadTuples) * 100
		}

		metrics = append(metrics, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return metrics, nil
}

// RecordHealthMetrics saves health metrics to database_health table for historical tracking.
func RecordHealthMetrics(ctx context.Context, pool *pgxpool.Pool, metrics []HealthMetrics) error {
	if len(metrics) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, m := range metrics {
		batch.Queue(`
			INSERT INTO database_health (
				table_name, total_size, index_size, 
				dead_tuples, live_tuples, 
				last_vacuum, last_autovacuum
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, m.TableName, m.TotalSize, m.IndexSize,
			m.DeadTuples, m.LiveTuples,
			m.LastVacuum, m.LastAutovacuum)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(metrics); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("record health metrics batch: %w", err)
		}
	}

	return nil
}

// PrintHealthMetrics displays health metrics in a formatted table.
func PrintHealthMetrics(metrics []HealthMetrics) {
	if len(metrics) == 0 {
		fmt.Println("\n[INFO] No health metrics available")
		return
	}

	top := "  ╔═══════════════════════════════════════════════════════════════════════════════╗"
	line := "  ├───────────────────────────────────────────────────────────────────────────────┤"
	bot := "  ╚═══════════════════════════════════════════════════════════════════════════════╝"
	title := "  ║                          DATABASE HEALTH METRICS                             ║"

	fmt.Println()
	fmt.Println(top)
	fmt.Println(title)
	fmt.Println(line)

	for _, m := range metrics {
		fmt.Printf("  ║  Table: %-68s ║\n", m.TableName)
		fmt.Println(line)
		fmt.Printf("  ║    Total Size      : %-54s ║\n", FormatBytes(m.TotalSize))
		fmt.Printf("  ║    Index Size      : %-54s ║\n", FormatBytes(m.IndexSize))
		fmt.Printf("  ║    Data Size       : %-54s ║\n", FormatBytes(m.TotalSize-m.IndexSize))
		fmt.Printf("  ║    Live Tuples     : %-54d ║\n", m.LiveTuples)
		fmt.Printf("  ║    Dead Tuples     : %-54d ║\n", m.DeadTuples)

		bloatStatus := fmt.Sprintf("%.1f%%", m.BloatPercent)
		if m.BloatPercent > 20 {
			bloatStatus += " ⚠️  HIGH - Consider VACUUM"
		} else if m.BloatPercent > 10 {
			bloatStatus += " ⚡ MODERATE"
		} else {
			bloatStatus += " ✓ HEALTHY"
		}
		fmt.Printf("  ║    Bloat           : %-54s ║\n", bloatStatus)

		if m.LastVacuum != nil {
			fmt.Printf("  ║    Last VACUUM     : %-54s ║\n", m.LastVacuum.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  ║    Last VACUUM     : %-54s ║\n", "Never")
		}

		if m.LastAutovacuum != nil {
			fmt.Printf("  ║    Last Autovacuum : %-54s ║\n", m.LastAutovacuum.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  ║    Last Autovacuum : %-54s ║\n", "Never")
		}

		fmt.Println(line)
	}

	fmt.Println(bot)
	fmt.Println()

	needsVacuum := false
	for _, m := range metrics {
		if m.BloatPercent > 20 {
			needsVacuum = true
			break
		}
	}

	if needsVacuum {
		log.Println("[WARN] Some tables have high bloat (>20%). Consider running VACUUM ANALYZE.")
		log.Println("[INFO] To vacuum: psql -d <database> -c 'VACUUM ANALYZE;'")
	}
}

// ============================================================================
// Retry Logic
// ============================================================================

// RetryConfig holds retry configuration parameters.
type RetryConfig struct {
	MaxAttempts  int           // Maximum number of retry attempts
	InitialDelay time.Duration // Initial delay between retries
	MaxDelay     time.Duration // Maximum delay between retries
	Multiplier   float64       // Backoff multiplier
}

// DefaultRetryConfig returns sensible defaults for database operations.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: RetryInitialDelay,
		MaxDelay:     RetryMaxDelay,
		Multiplier:   2.0,
	}
}

// RetryableFunc is a function that can be retried.
type RetryableFunc func() error

// WithRetry executes a function with exponential backoff retry logic.
// ponytail: Uses stdlib time package, no new dependencies.
func WithRetry(ctx context.Context, cfg RetryConfig, fn RetryableFunc) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context cancellation before attempting
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		default:
		}

		// Execute the function
		err := fn()
		if err == nil {
			// Success
			if attempt > 1 {
				log.Printf("[INFO] Operation succeeded on attempt %d/%d", attempt, cfg.MaxAttempts)
			}
			return nil
		}

		lastErr = err

		// Don't retry on last attempt
		if attempt == cfg.MaxAttempts {
			break
		}

		// Log retry attempt
		log.Printf("[WARN] Operation failed (attempt %d/%d): %v. Retrying in %v...",
			attempt, cfg.MaxAttempts, err, delay)

		// Wait with exponential backoff
		select {
		case <-time.After(delay):
			// Calculate next delay with exponential backoff
			delay = time.Duration(float64(delay) * cfg.Multiplier)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled during backoff: %w", ctx.Err())
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", cfg.MaxAttempts, lastErr)
}

