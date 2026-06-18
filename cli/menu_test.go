package cli

import "testing"

func TestGenerationTotalUsesConfiguredBatchSize(t *testing.T) {
	total, ok := generationTotal(25, 1000)
	if !ok || total != 25000 {
		t.Fatalf("generationTotal(25, 1000) = %d, %v; want 25000, true", total, ok)
	}
}

func TestGenerationTotalRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		batches   int
		batchSize int
	}{
		{0, 1000},
		{25, 0},
	} {
		if total, ok := generationTotal(tc.batches, tc.batchSize); ok {
			t.Fatalf("generationTotal(%d, %d) = %d, true; want false", tc.batches, tc.batchSize, total)
		}
	}
}

func TestValidateBatchSizeMatchesConfigLimits(t *testing.T) {
	for _, n := range []int{1, 500, 1000} {
		if err := validateBatchSize(n); err != nil {
			t.Fatalf("validateBatchSize(%d) returned error: %v", n, err)
		}
	}

	for _, n := range []int{0, 1001} {
		if err := validateBatchSize(n); err == nil {
			t.Fatalf("validateBatchSize(%d) returned nil; want error", n)
		}
	}
}
