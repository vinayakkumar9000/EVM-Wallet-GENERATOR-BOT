// Package core — parallel wallet generation engine.
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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"evmwalletbot/config"
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
// into PostgreSQL using COPY protocol, and updates a single terminal line in-place.
func GenerateWallets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, totalWallets int) error {
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

			// Retry database insert with exponential backoff
			var ids []int64
			retryErr := WithRetry(ctx, DefaultRetryConfig(), func() error {
				var err error
				ids, err = insertWalletBatchCopy(pool, batch)
				return err
			})

			if retryErr != nil {
				close(progressDone)
				cancel()     // Cancel context to stop workers
				genWG.Wait() // Wait for all workers to finish
				return fmt.Errorf("DB insert (batch %d) failed after retries: %w", batchNum, retryErr)
			}
			confirmedCount.Add(int64(len(ids)))
			batchesCompleted.Add(1)

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

		// Retry database insert with exponential backoff
		var ids []int64
		retryErr := WithRetry(ctx, DefaultRetryConfig(), func() error {
			var err error
			ids, err = insertWalletBatchCopy(pool, batch)
			return err
		})

		if retryErr != nil {
			close(progressDone)
			cancel()     // Cancel context to stop workers
			genWG.Wait() // Wait for all workers to finish
			return fmt.Errorf("DB insert (final batch) failed after retries: %w", retryErr)
		}
		confirmedCount.Add(int64(len(ids)))
		batchesCompleted.Add(1)

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
	tracker.Finish(done)

	return nil
}

// insertWalletBatchCopy uses PostgreSQL COPY protocol for maximum throughput.
// ponytail: COPY is 3-5× faster than multi-row INSERT for bulk data.
// Uses UNLOGGED table for staging to skip WAL writes (20-30% faster).
func insertWalletBatchCopy(pool *pgxpool.Pool, wallets []*wallet.Wallet) ([]int64, error) {
	if len(wallets) == 0 {
		return nil, nil
	}

	// ponytail: Add timeout for long-running COPY operations
	ctx, cancel := context.WithTimeout(context.Background(), HealthCheckTimeout)
	defer cancel()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}

	// Step 1: Create or truncate UNLOGGED staging table (faster than TEMP)
	// ponytail: UNLOGGED skips WAL, reusable across batches
	_, err = tx.Exec(ctx, `
		CREATE UNLOGGED TABLE IF NOT EXISTS wallet_staging_reusable (
			address     BYTEA,
			private_key BYTEA
		)
	`)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			log.Printf("[ERROR] Rollback failed after create staging table error: %v", rbErr)
		}
		return nil, fmt.Errorf("create staging table: %w", err)
	}

	// Truncate for reuse (faster than DROP/CREATE)
	_, err = tx.Exec(ctx, `TRUNCATE wallet_staging_reusable`)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			log.Printf("[ERROR] Rollback failed after truncate staging table error: %v", rbErr)
		}
		return nil, fmt.Errorf("truncate staging table: %w", err)
	}

	// Step 2: Use COPY protocol to bulk-load data into reusable staging table
	_, err = tx.CopyFrom(
		ctx,
		pgx.Identifier{"wallet_staging_reusable"},
		[]string{"address", "private_key"},
		pgx.CopyFromSlice(len(wallets), func(i int) ([]interface{}, error) {
			return []interface{}{wallets[i].Address, wallets[i].PrivateKey}, nil
		}),
	)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			log.Printf("[ERROR] Rollback failed after copy data error: %v", rbErr)
		}
		return nil, fmt.Errorf("copy data: %w", err)
	}

	// Step 3: INSERT from staging into main table with RETURNING id
	rows, err := tx.Query(ctx, `
		INSERT INTO wallets (address, private_key)
		SELECT address, private_key FROM wallet_staging_reusable
		RETURNING id
	`)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			log.Printf("[ERROR] Rollback failed after insert from staging error: %v", rbErr)
		}
		return nil, fmt.Errorf("insert from staging: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, len(wallets))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				log.Printf("[ERROR] Rollback failed after scan id error: %v", rbErr)
			}
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			log.Printf("[ERROR] Rollback failed after rows error: %v", rbErr)
		}
		return nil, err
	}

	// Commit transaction - no rollback after this point
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return ids, nil
}

// ponytail: Removed logCreationEvents - replaced with simple log.Printf for batch completion.
// This eliminates the need for the events package dependency in the generator.
