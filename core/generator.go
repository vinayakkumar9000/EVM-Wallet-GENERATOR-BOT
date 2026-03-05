// Package core — parallel wallet generation engine.
package core

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"evmwalletbot/config"
	"evmwalletbot/events"
	"evmwalletbot/wallet"
)

// GenerateWallets generates `totalWallets` EVM wallets in parallel, inserts them
// into PostgreSQL in high-performance batches, and updates a single terminal
// line in-place (no line flooding).
func GenerateWallets(db *sql.DB, cfg *config.Config, totalWallets int) error {
	start := time.Now()

	workers := cfg.Workers
	if workers > totalWallets {
		workers = totalWallets
	}
	if workers < 1 {
		workers = 1
	}

	log.Printf("[INFO] Generating %d wallets | workers=%d | DB chunk=%d\n",
		totalWallets, workers, cfg.BatchSize)

	// ── Smooth in-place progress ticker (BUG FIX #5) ─────────────────────
	// An atomic counter is incremented by the batch inserter each time rows
	// are confirmed by the DB.  A separate goroutine reads it every 200ms and
	// rewrites the same terminal line — no flooding, no waiting until the end
	// of a full DB batch to see movement.
	var confirmedCount atomic.Int64
	progressDone := make(chan struct{})

	fmt.Printf("\n")
	printProgress(0, totalWallets) // show 0% immediately

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				printProgress(int(confirmedCount.Load()), totalWallets)
			case <-progressDone:
				// Print the definitive final value before exiting.
				printProgress(int(confirmedCount.Load()), totalWallets)
				return
			}
		}
	}()

	// ── Parallel key-generation goroutines ───────────────────────────────
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

	// ── Sequential batch inserter ─────────────────────────────────────────
	// Reads wallets from channel, flushes to PostgreSQL in multi-row INSERT
	// batches, then fires async event-logging goroutines (tracked by eventWG
	// so we wait for them before returning — no fire-and-forget leaks).
	var eventWG sync.WaitGroup

	batch := make([]*wallet.Wallet, 0, cfg.BatchSize)
	batchNum := 0

	for w := range walletCh {
		batch = append(batch, w)

		if len(batch) >= cfg.BatchSize {
			batchNum++
			ids, err := insertWalletBatch(db, batch)
			if err != nil {
				close(progressDone)
				eventWG.Wait()
				return fmt.Errorf("DB insert (batch %d): %w", batchNum, err)
			}

			// Advance the atomic counter with confirmed DB rows, not sent rows.
			confirmedCount.Add(int64(len(ids)))

			eventWG.Add(1)
			go func(capturedIDs []int64, bn int) {
				defer eventWG.Done()
				logCreationEvents(db, capturedIDs, bn)
			}(ids, batchNum)

			batch = batch[:0]
		}
	}

	// ── Flush remainder ───────────────────────────────────────────────────
	if len(batch) > 0 {
		batchNum++
		ids, err := insertWalletBatch(db, batch)
		if err != nil {
			close(progressDone)
			eventWG.Wait()
			return fmt.Errorf("DB insert (final batch): %w", err)
		}

		confirmedCount.Add(int64(len(ids)))

		eventWG.Add(1)
		go func(capturedIDs []int64, bn int) {
			defer eventWG.Done()
			logCreationEvents(db, capturedIDs, bn)
		}(ids, batchNum)
	}

	// Stop the progress ticker and print the final 100% line.
	close(progressDone)
	// Give the ticker goroutine a moment to print the final value.
	time.Sleep(50 * time.Millisecond)

	// Wait for all event goroutines to finish before returning so stats are
	// immediately consistent after GenerateWallets returns.
	eventWG.Wait()

	done := int(confirmedCount.Load())
	elapsed := time.Since(start)
	rate := float64(done) / elapsed.Seconds()
	fmt.Printf("\n\n[INFO] %d wallets successfully created in %.2fs  (%.0f wallets/sec)\n\n",
		done, elapsed.Seconds(), rate)

	return nil
}

// printProgress rewrites the current terminal line in-place using \r.
// Trailing spaces ensure leftover characters from a previous longer line
// are fully erased.  No newline is emitted — the cursor stays on the same row.
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

// insertWalletBatch performs a single multi-row INSERT … RETURNING id inside
// an explicit transaction.
//
// BUG FIX #1 — rows.Close() is called EXPLICITLY before tx.Commit().
// lib/pq returns "unexpected SimpleQuery response" if a result-set cursor is
// still open when the transaction is committed.  Using defer rows.Close()
// would close it AFTER the return statement executes tx.Commit(), which is
// too late.
func insertWalletBatch(db *sql.DB, wallets []*wallet.Wallet) ([]int64, error) {
	if len(wallets) == 0 {
		return nil, nil
	}

	placeholders := make([]string, 0, len(wallets))
	args := make([]interface{}, 0, len(wallets)*2)

	for i, w := range wallets {
		placeholders = append(placeholders,
			fmt.Sprintf("($%d,$%d)", i*2+1, i*2+2),
		)
		args = append(args, w.Address, w.PrivateKey)
	}

	query := "INSERT INTO wallets (address, private_key) VALUES " +
		strings.Join(placeholders, ",") + " RETURNING id"

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("exec insert: %w", err)
	}

	ids := make([]int64, 0, len(wallets))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}

	// BUG FIX #1 — close the result-set BEFORE committing the transaction.
	rows.Close()

	return ids, tx.Commit()
}

// logCreationEvents fires a bulk event insert for all wallets in a batch.
func logCreationEvents(db *sql.DB, ids []int64, batchNum int) {
	data := map[string]interface{}{
		"batch":  batchNum,
		"count":  len(ids),
		"source": "generator",
	}
	if err := events.LogBatch(db, ids, events.WalletCreated, data); err != nil {
		log.Printf("[WARN] Event logging failed for batch %d: %v", batchNum, err)
	}
}
