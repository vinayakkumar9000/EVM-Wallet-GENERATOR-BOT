package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestWithRetry verifies basic retry functionality
func TestWithRetry(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	// Test successful operation on first attempt
	t.Run("success on first attempt", func(t *testing.T) {
		attempts := 0
		err := WithRetry(ctx, cfg, func() error {
			attempts++
			return nil
		})
		if err != nil {
			t.Errorf("WithRetry() failed: %v", err)
		}
		if attempts != 1 {
			t.Errorf("Expected 1 attempt, got %d", attempts)
		}
	})

	// Test successful operation on second attempt
	t.Run("success on second attempt", func(t *testing.T) {
		attempts := 0
		err := WithRetry(ctx, cfg, func() error {
			attempts++
			if attempts < 2 {
				return errors.New("temporary error")
			}
			return nil
		})
		if err != nil {
			t.Errorf("WithRetry() failed: %v", err)
		}
		if attempts != 2 {
			t.Errorf("Expected 2 attempts, got %d", attempts)
		}
	})

	// Test failure after max attempts
	t.Run("failure after max attempts", func(t *testing.T) {
		attempts := 0
		err := WithRetry(ctx, cfg, func() error {
			attempts++
			return errors.New("persistent error")
		})
		if err == nil {
			t.Error("WithRetry() should have failed")
		}
		if attempts != cfg.MaxAttempts {
			t.Errorf("Expected %d attempts, got %d", cfg.MaxAttempts, attempts)
		}
	})
}

// TestWithRetryContextCancellation verifies context cancellation handling
func TestWithRetryContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RetryConfig{
		MaxAttempts:  10,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		Multiplier:   2.0,
	}

	// Cancel context after first attempt
	attempts := 0
	err := WithRetry(ctx, cfg, func() error {
		attempts++
		if attempts == 1 {
			cancel()
		}
		return errors.New("error")
	})

	if err == nil {
		t.Error("WithRetry() should have failed due to context cancellation")
	}
	if attempts > 2 {
		t.Errorf("Expected at most 2 attempts, got %d", attempts)
	}
}

// TestDefaultRetryConfig verifies default configuration
func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != RetryInitialDelay {
		t.Errorf("InitialDelay = %v, want %v", cfg.InitialDelay, RetryInitialDelay)
	}
	if cfg.MaxDelay != RetryMaxDelay {
		t.Errorf("MaxDelay = %v, want %v", cfg.MaxDelay, RetryMaxDelay)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("Multiplier = %f, want 2.0", cfg.Multiplier)
	}
}

// TestIsRetryable verifies error classification
func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "connection reset",
			err:      errors.New("connection reset by peer"),
			expected: true,
		},
		{
			name:     "timeout",
			err:      errors.New("operation timeout"),
			expected: true,
		},
		{
			name:     "temporary failure",
			err:      errors.New("temporary failure in name resolution"),
			expected: true,
		},
		{
			name:     "too many connections",
			err:      errors.New("too many connections"),
			expected: true,
		},
		{
			name:     "deadlock detected",
			err:      errors.New("deadlock detected"),
			expected: true,
		},
		{
			name:     "serialization error",
			err:      errors.New("could not serialize access"),
			expected: true,
		},
		{
			name:     "connection closed",
			err:      errors.New("connection closed"),
			expected: true,
		},
		{
			name:     "broken pipe",
			err:      errors.New("broken pipe"),
			expected: true,
		},
		{
			name:     "no such host",
			err:      errors.New("no such host"),
			expected: true,
		},
		{
			name:     "non-retryable error",
			err:      errors.New("invalid syntax"),
			expected: false,
		},
		{
			name:     "permission denied",
			err:      errors.New("permission denied"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryable(tt.err)
			if got != tt.expected {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

// TestCalculateBackoff verifies exponential backoff calculation
func TestCalculateBackoff(t *testing.T) {
	cfg := RetryConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 100 * time.Millisecond},
		{2, 200 * time.Millisecond},
		{3, 400 * time.Millisecond},
		{4, 800 * time.Millisecond},
		{5, 1 * time.Second}, // Capped at MaxDelay
		{6, 1 * time.Second}, // Capped at MaxDelay
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := CalculateBackoff(tt.attempt, cfg)
			if got != tt.expected {
				t.Errorf("CalculateBackoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
			}
		})
	}
}

// TestWithRetryOnTransient verifies transient error retry logic
func TestWithRetryOnTransient(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     100 * time.Millisecond,
		Multiplier:   2.0,
	}

	// Test retryable error
	t.Run("retryable error", func(t *testing.T) {
		attempts := 0
		err := WithRetryOnTransient(ctx, cfg, func() error {
			attempts++
			if attempts < 2 {
				return errors.New("connection refused")
			}
			return nil
		})
		if err != nil {
			t.Errorf("WithRetryOnTransient() failed: %v", err)
		}
		if attempts != 2 {
			t.Errorf("Expected 2 attempts, got %d", attempts)
		}
	})

	// Test non-retryable error (should fail immediately)
	t.Run("non-retryable error", func(t *testing.T) {
		attempts := 0
		err := WithRetryOnTransient(ctx, cfg, func() error {
			attempts++
			return errors.New("invalid syntax")
		})
		if err == nil {
			t.Error("WithRetryOnTransient() should have failed")
		}
		if attempts != 1 {
			t.Errorf("Expected 1 attempt (fail fast), got %d", attempts)
		}
	})
}

// BenchmarkWithRetry measures retry overhead
func BenchmarkWithRetry(b *testing.B) {
	ctx := context.Background()
	cfg := DefaultRetryConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WithRetry(ctx, cfg, func() error {
			return nil
		})
	}
}

// BenchmarkIsRetryable measures error classification performance
func BenchmarkIsRetryable(b *testing.B) {
	err := errors.New("connection refused")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = IsRetryable(err)
	}
}
