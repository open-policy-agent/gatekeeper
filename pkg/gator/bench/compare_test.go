package bench

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadResults(t *testing.T) {
	results := []Results{
		{
			Engine:          EngineRego,
			TemplateCount:   5,
			ConstraintCount: 10,
			ObjectCount:     100,
			Iterations:      50,
			SetupDuration:   time.Second,
			TotalDuration:   5 * time.Second,
			Latencies: Latencies{
				Min:  100 * time.Microsecond,
				Max:  10 * time.Millisecond,
				Mean: 1 * time.Millisecond,
				P50:  900 * time.Microsecond,
				P95:  5 * time.Millisecond,
				P99:  8 * time.Millisecond,
			},
			ViolationCount:   25,
			ReviewsPerSecond: 1000,
			MemoryStats: &MemoryStats{
				AllocsPerReview: 500,
				BytesPerReview:  10240,
				TotalAllocs:     25000,
				TotalBytes:      512000,
			},
		},
	}

	t.Run("JSON format", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "baseline.json")

		// Save
		err := SaveResults(results, path)
		if err != nil {
			t.Fatalf("SaveResults failed: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatalf("file was not created")
		}

		// Load
		loaded, err := LoadBaseline(path)
		if err != nil {
			t.Fatalf("LoadBaseline failed: %v", err)
		}

		if len(loaded) != 1 {
			t.Fatalf("expected 1 result, got %d", len(loaded))
		}

		if loaded[0].Engine != EngineRego {
			t.Errorf("Engine = %v, want %v", loaded[0].Engine, EngineRego)
		}
		if loaded[0].ReviewsPerSecond != 1000 {
			t.Errorf("ReviewsPerSecond = %v, want %v", loaded[0].ReviewsPerSecond, 1000)
		}
	})

	t.Run("YAML format", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "baseline.yaml")

		// Save
		err := SaveResults(results, path)
		if err != nil {
			t.Fatalf("SaveResults failed: %v", err)
		}

		// Load
		loaded, err := LoadBaseline(path)
		if err != nil {
			t.Fatalf("LoadBaseline failed: %v", err)
		}

		if len(loaded) != 1 {
			t.Fatalf("expected 1 result, got %d", len(loaded))
		}

		if loaded[0].Engine != EngineRego {
			t.Errorf("Engine = %v, want %v", loaded[0].Engine, EngineRego)
		}
	})

	t.Run("YML extension", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "baseline.yml")

		// Save
		err := SaveResults(results, path)
		if err != nil {
			t.Fatalf("SaveResults failed: %v", err)
		}

		// Load
		loaded, err := LoadBaseline(path)
		if err != nil {
			t.Fatalf("LoadBaseline failed: %v", err)
		}

		if len(loaded) != 1 {
			t.Fatalf("expected 1 result, got %d", len(loaded))
		}
	})
}

func TestLoadBaseline_FileNotFound(t *testing.T) {
	_, err := LoadBaseline("/nonexistent/path/baseline.json")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestCompare(t *testing.T) {
	baseline := []Results{
		{
			Engine: EngineRego,
			Latencies: Latencies{
				P50:  1 * time.Millisecond,
				P95:  5 * time.Millisecond,
				P99:  10 * time.Millisecond,
				Mean: 2 * time.Millisecond,
			},
			ReviewsPerSecond: 1000,
			MemoryStats: &MemoryStats{
				AllocsPerReview: 500,
				BytesPerReview:  10240,
			},
		},
	}

	t.Run("no regression", func(t *testing.T) {
		current := []Results{
			{
				Engine: EngineRego,
				Latencies: Latencies{
					P50:  1050 * time.Microsecond, // 5% increase
					P95:  5 * time.Millisecond,
					P99:  10 * time.Millisecond,
					Mean: 2 * time.Millisecond,
				},
				ReviewsPerSecond: 950, // 5% decrease
				MemoryStats: &MemoryStats{
					AllocsPerReview: 520, // 4% increase
					BytesPerReview:  10500,
				},
			},
		}

		comparisons := Compare(baseline, current, 10.0)
		if len(comparisons) != 1 {
			t.Fatalf("expected 1 comparison, got %d", len(comparisons))
		}

		if !comparisons[0].Passed {
			t.Errorf("expected comparison to pass, got failed metrics: %v", comparisons[0].FailedMetrics)
		}
	})

	t.Run("latency regression", func(t *testing.T) {
		current := []Results{
			{
				Engine: EngineRego,
				Latencies: Latencies{
					P50:  1500 * time.Microsecond, // 50% increase - regression!
					P95:  5 * time.Millisecond,
					P99:  10 * time.Millisecond,
					Mean: 2 * time.Millisecond,
				},
				ReviewsPerSecond: 1000,
			},
		}

		comparisons := Compare(baseline, current, 10.0)
		if len(comparisons) != 1 {
			t.Fatalf("expected 1 comparison, got %d", len(comparisons))
		}

		if comparisons[0].Passed {
			t.Error("expected comparison to fail due to latency regression")
		}
		if len(comparisons[0].FailedMetrics) == 0 {
			t.Error("expected failed metrics to be populated")
		}
	})

	t.Run("throughput regression", func(t *testing.T) {
		current := []Results{
			{
				Engine: EngineRego,
				Latencies: Latencies{
					P50:  1 * time.Millisecond,
					P95:  5 * time.Millisecond,
					P99:  10 * time.Millisecond,
					Mean: 2 * time.Millisecond,
				},
				ReviewsPerSecond: 800, // 20% decrease - regression!
			},
		}

		comparisons := Compare(baseline, current, 10.0)
		if len(comparisons) != 1 {
			t.Fatalf("expected 1 comparison, got %d", len(comparisons))
		}

		if comparisons[0].Passed {
			t.Error("expected comparison to fail due to throughput regression")
		}

		foundThroughput := false
		for _, m := range comparisons[0].FailedMetrics {
			if m == "Throughput" {
				foundThroughput = true
				break
			}
		}
		if !foundThroughput {
			t.Error("expected Throughput to be in failed metrics")
		}
	})

	t.Run("no matching engine", func(t *testing.T) {
		current := []Results{
			{
				Engine: EngineCEL, // Different engine
				Latencies: Latencies{
					P50: 1 * time.Millisecond,
				},
				ReviewsPerSecond: 1000,
			},
		}

		comparisons := Compare(baseline, current, 10.0)
		if len(comparisons) != 0 {
			t.Errorf("expected 0 comparisons for non-matching engine, got %d", len(comparisons))
		}
	})
}

func TestCalculateDelta(t *testing.T) {
	tests := []struct {
		name     string
		baseline float64
		current  float64
		want     float64
	}{
		{
			name:     "no change",
			baseline: 100,
			current:  100,
			want:     0,
		},
		{
			name:     "10% increase",
			baseline: 100,
			current:  110,
			want:     10,
		},
		{
			name:     "10% decrease",
			baseline: 100,
			current:  90,
			want:     -10,
		},
		{
			name:     "zero baseline with current",
			baseline: 0,
			current:  100,
			want:     100,
		},
		{
			name:     "both zero",
			baseline: 0,
			current:  0,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateDelta(tt.baseline, tt.current)
			if got != tt.want {
				t.Errorf("calculateDelta(%v, %v) = %v, want %v",
					tt.baseline, tt.current, got, tt.want)
			}
		})
	}
}

func TestFormatComparison(t *testing.T) {
	comparisons := []ComparisonResult{
		{
			BaselineEngine: EngineRego,
			CurrentEngine:  EngineRego,
			Metrics: []MetricComparison{
				{Name: "P50 Latency", Baseline: 1000000, Current: 1100000, Delta: 10, Passed: true},
				{Name: "Throughput", Baseline: 1000, Current: 900, Delta: -10, Passed: true},
			},
			Passed:        true,
			FailedMetrics: nil,
		},
	}

	output := FormatComparison(comparisons, 10.0)

	// Check that output contains expected strings
	if output == "" {
		t.Error("expected non-empty output")
	}

	expectedStrings := []string{
		"Baseline Comparison",
		"REGO",
		"P50 Latency",
		"Throughput",
		"No significant regressions",
	}

	for _, s := range expectedStrings {
		if !containsString(output, s) {
			t.Errorf("expected output to contain %q", s)
		}
	}
}

func TestFormatComparison_WithRegression(t *testing.T) {
	comparisons := []ComparisonResult{
		{
			BaselineEngine: EngineRego,
			CurrentEngine:  EngineRego,
			Metrics: []MetricComparison{
				{Name: "P50 Latency", Baseline: 1000000, Current: 1500000, Delta: 50, Passed: false},
			},
			Passed:        false,
			FailedMetrics: []string{"P50 Latency"},
		},
	}

	output := FormatComparison(comparisons, 10.0)

	expectedStrings := []string{
		"REGRESSION",
		"Regressions detected",
		"P50 Latency",
	}

	for _, s := range expectedStrings {
		if !containsString(output, s) {
			t.Errorf("expected output to contain %q", s)
		}
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
