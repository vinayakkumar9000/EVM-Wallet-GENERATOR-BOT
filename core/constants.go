// Package core — configuration constants for the wallet generator.
package core

import "time"

// ponytail: Extracted magic numbers to named constants for maintainability.
// These are sensible defaults that work well for most systems.
// Upgrade: Make configurable via environment variables if needed.

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

	// Connection pool lifecycle
	MaxConnLifetime   = 5 * time.Minute
	MaxConnIdleTime   = 2 * time.Minute
	HealthCheckPeriod = 1 * time.Minute

	// Pool warmup configuration
	// ponytail: Reduced from 1000/32 to 256/16 to lower memory footprint
	// while maintaining performance. Pool grows naturally during runtime for large batches.
	MinPoolWarmup    = 100 // Minimum objects to pre-allocate
	MaxPoolWarmup    = 256 // Maximum objects to pre-allocate (reduced from 1000)
	WarmupMultiplier = 16  // Objects per CPU core (reduced from 32)
)
