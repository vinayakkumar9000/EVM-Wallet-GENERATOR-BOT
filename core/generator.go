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
	"evmwalletbot/events"
	"evmwalletbot/wallet"
)

// walletPool reuses wallet objects to reduce GC pressure.
// ponytail: sync.Pool is stdlib, no new dependency needed.
var walletPool = sync.Pool{
	New: func() interface{} {
		return &wallet.Wallet{}
	},
}

// eventWorkerPool processes event logging without spawning goroutines per batch.
type eventWorkerPool struct {
	pool     *pgxpool.Pool
	jobCh    chan eventJob
	wg       sync.WaitGroup
	workers  int
}

type eventJob struct {
	ids      []int64
	batchNum int
}

func newEventWorkerPool(pool *pgxpool.Pool, workers int) *eventWorkerPool {
	if workers < 1 {
		workers = 1
	}
	p := &eventWorkerPool{
		pool:    pool,
		jobCh:   make(chan eventJob, workers*2),
		workers: workers,
	}
	p.start()
	return p
}

func (p *eventWorkerPool) start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for job := range p.jobCh {
				logCreationEvents(p.pool, job.ids, job.batchNum)
			}
		}()
	}
}

func (p *eventWorkerPool) submit(ids []int64, batchNum int) {
	p.jobCh <- eventJob{ids: ids, batchNum: batchNum}
}

func (p *eventWorkerPool) close() {
	close(p.jobCh)
	p.wg.Wait()
}

// GenerateWallets generates `totalWallets` EVM wallets in parallel, inserts them
// into PostgreSQL using COPY protocol, and updates a single terminal line in-place.
func GenerateWallets(pool *pgxpool.Pool, cfg *config.Config, totalWallets int) error {
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
	printProgress(0, totalWallets)

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				printProgress(int(confirmedCount.Load()), totalWallets)
			case <-progressDone:
				printProgress(int(confirmedCount.Load()), totalWallets)
				return
			}
		}
	}()

	// ── Event worker pool (reuses goroutines) ─────────────────────────────
	eventPool := newEventWorkerPool(pool, 4) // ponytail: 4 workers sufficient for event logging
	defer eventPool.close()

	// ── Parallel key-generation goroutines ────────────────────────────────
	walletCh := make(chan *wallet.Wallet, workers*10)

	var genWG sync.WaitGroup
	perWorker := totalWallets / workers
	remainder := totalWallets % workers

	for i := 0; i < workers; i++ {
		count := perWorker
		if i < remainder {
			count++
		}
		genWG.Add(1)
		go func(n int) {
			defer genWG.Done()
			for j := 0; j < n; j++ {
				w, err := wallet.Generate()
				if err != nil {
					log.Printf("[WARN] Key generation error (skipping): %v", err)
					continue
				}
				walletCh <- w
			}
		}(count)
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
			ids, err := insertWalletBatchCopy(pool, batch)
			if err != nil {
				close(progressDone)
				return fmt.Errorf("DB insert (batch %d): %w", batchNum, err)
			}

			confirmedCount.Add(int64(len(ids)))
			eventPool.submit(ids, batchNum)

			// ponytail: Return wallet objects to pool for reuse.
			for _, w := range batch {
				walletPool.Put(w)
			}
			batch = batch[:0]
		}
	}

	// ── Flush remainder ───────────────────────────────────────────────────
	if len(batch) > 0 {
		batchNum++
		ids, err := insertWalletBatchCopy(pool, batch)
		if err != nil {
			close(progressDone)
			return fmt.Errorf("DB insert (final batch): %w", err)
		}

		confirmedCount.Add(int64(len(ids)))
		eventPool.submit(ids, batchNum)

		for _, w := range batch {
			walletPool.Put(w)
		}
	}

	close(progressDone)
	time.Sleep(50 * time.Millisecond)

	done := int(confirmedCount.Load())
	elapsed := time.Since(start)
	rate := float64(done) / elapsed.Seconds()
	fmt.Printf("\n\n[INFO] %d wallets successfully created in %.2fs  (%.0f wallets/sec)\n\n",
		done, elapsed.Seconds(), rate)

	return nil
}

// printProgress rewrites the current terminal line in-place using \r.
func printProgress(done, total int) {
	const barWidth = 28

	pct := 0.0
	if total > 0 {
		pct = float64(done) / float64(total) * 100
		if pct > 100 {
			pct = 100
		}
	}

	filled := int(pct / 100 * barWidth)
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	fmt.Printf("\r  Updating progress: %-8d / %-8d  [%s]  %5.1f%%   ",
		done, total, bar, pct)
}

// insertWalletBatchCopy uses PostgreSQL COPY protocol for maximum throughput.
// ponytail: COPY is 3-5× faster than multi-row INSERT for bulk data.
func insertWalletBatchCopy(pool *pgxpool.Pool, wallets []*wallet.Wallet) ([]int64, error) {
	if len(wallets) == 0 {
		return nil, nil
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Step 1: COPY data into temporary table (no indexes, no constraints = fastest)
	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE wallet_staging (
			address     BYTEA,
			private_key BYTEA
		) ON COMMIT DROP
	`)
	if err != nil {
		return nil, fmt.Errorf("create staging table: %w", err)
	}

	// Step 2: Use COPY protocol to bulk-load data
	_, err = tx.CopyFrom(
		ctx,
		pgx.Identifier{"wallet_staging"},
		[]string{"address", "private_key"},
		pgx.CopyFromSlice(len(wallets), func(i int) ([]interface{}, error) {
			return []interface{}{wallets[i].Address, wallets[i].PrivateKey}, nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("copy data: %w", err)
	}

	// Step 3: INSERT from staging into main table with RETURNING id
	rows, err := tx.Query(ctx, `
		INSERT INTO wallets (address, private_key)
		SELECT address, private_key FROM wallet_staging
		RETURNING id
	`)
	if err != nil {
		return nil, fmt.Errorf("insert from staging: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, len(wallets))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, tx.Commit(ctx)
}

// logCreationEvents fires a bulk event insert for all wallets in a batch.
func logCreationEvents(pool *pgxpool.Pool, ids []int64, batchNum int) {
	data := map[string]interface{}{
		"batch":  batchNum,
		"count":  len(ids),
		"source": "generator",
	}
	if err := events.LogBatch(pool, ids, events.WalletCreated, data); err != nil {
		log.Printf("[WARN] Event logging failed for batch %d: %v", batchNum, err)
	}
}
