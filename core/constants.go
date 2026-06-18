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
	MinPoolWarmup = 100  // Minimum objects to pre-allocate
	MaxPoolWarmup = 1000 // Maximum objects to pre-allocate
	WarmupMultiplier = 32 // Objects per CPU core
)
