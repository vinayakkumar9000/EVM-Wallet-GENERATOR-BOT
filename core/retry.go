// Package core — retry logic with exponential backoff.
package core

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"
)

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

// IsRetryable determines if an error should trigger a retry.
// ponytail: Common transient errors that benefit from retry.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Network-related errors
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"too many connections",
		"deadlock detected",
		"could not serialize",
		"connection closed",
		"broken pipe",
		"no such host",
	}

	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// WithRetryOnTransient wraps WithRetry but only retries on transient errors.
func WithRetryOnTransient(ctx context.Context, cfg RetryConfig, fn RetryableFunc) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("retry cancelled: %w", ctx.Err())
		default:
		}

		err := fn()
		if err == nil {
			if attempt > 1 {
				log.Printf("[INFO] Operation succeeded on attempt %d/%d", attempt, cfg.MaxAttempts)
			}
			return nil
		}

		if !IsRetryable(err) {
			log.Printf("[INFO] Non-retryable error, failing immediately: %v", err)
			return err
		}

		lastErr = err
		if attempt == cfg.MaxAttempts {
			break
		}

		log.Printf("[WARN] Operation failed (attempt %d/%d): %v. Retrying in %v...",
			attempt, cfg.MaxAttempts, err, delay)

		select {
		case <-time.After(delay):
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

// CalculateBackoff calculates the delay for a given attempt number.
func CalculateBackoff(attempt int, cfg RetryConfig) time.Duration {
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	return time.Duration(delay)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				containsMiddle(s, substr))))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
