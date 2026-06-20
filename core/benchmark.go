// Package core — benchmark utilities for performance testing
package core

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"evmwalletbot/config"
	"evmwalletbot/wallet"
)

// BenchmarkWalletGeneration generates wallets for benchmarking WITHOUT storing them.
// This is used purely for performance measurement and tuning.
func BenchmarkWalletGeneration(ctx context.Context, cfg *config.Config, totalWallets int) (time.Duration, error) {
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
				w := walletPool.Get().(*wallet.Wallet)
				if err := wallet.GenerateInto(w); err != nil {
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
