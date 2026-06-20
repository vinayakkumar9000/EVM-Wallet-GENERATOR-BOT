// Package core — parallel wallet generation engine.
package core

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"evmwalletbot/config"
	"evmwalletbot/storage"
	"evmwalletbot/wallet"
)

// walletPool reuses wallet objects to reduce GC pressure.
// ponytail: sync.Pool is stdlib, no new dependency needed.
var walletPool = sync.Pool{
	New: func() interface{} {
		return &wallet.Wallet{
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
		walletPool.Put(&wallet.Wallet{
			Address:    make([]byte, 20),
			PrivateKey: make([]byte, 32),
		})
	}
}

// ponytail: Removed eventWorkerPool - batch-level logging is simpler and faster.
// Old approach: 1 event per wallet = 10M events for 10M wallets.
// New approach: 1 log per batch = ~20K logs for 10M wallets (500x reduction).

// GenerateWallets generates `totalWallets` EVM wallets in parallel, inserts them
// into the storage backend, and updates a single terminal line in-place.
func GenerateWallets(ctx context.Context, store storage.Storage, cfg *config.Config, totalWallets int) error {
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
	var exporter *wallet.Exporter
	var exportErr error
	if cfg.ExportEnabled {
		exporter, exportErr = wallet.NewExporter(*cfg)
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
	walletCh := make(chan *wallet.Wallet, cfg.BatchSize*2)

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
				w := walletPool.Get().(*wallet.Wallet)
				if err := wallet.GenerateInto(w); err != nil {
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
	batch := make([]*wallet.Wallet, 0, cfg.BatchSize)
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

// ponytail: Removed insertWalletBatchCopy - now handled by storage.Storage interface.
// PostgreSQL-specific COPY protocol is implemented in storage/postgres package.
// SQLite uses batch INSERT with transactions in storage/sqlite package.
