package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"sigs.k8s.io/yaml"
)

// SaveResults saves benchmark results to a file in JSON or YAML format.
// The format is determined by the file extension (.json or .yaml/.yml).
func SaveResults(results []Results, path string) error {
	ext := filepath.Ext(path)

	var data []byte
	var err error

	switch ext {
	case gator.ExtYAML, gator.ExtYML:
		data, err = yaml.Marshal(results)
	default:
		// Default to JSON
		data, err = json.MarshalIndent(results, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("marshaling results: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing results to %s: %w", path, err)
	}

	return nil
}

// LoadBaseline loads baseline results from a file.
// The format is determined by the file extension (.json or .yaml/.yml).
func LoadBaseline(path string) ([]Results, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading baseline from %s: %w", path, err)
	}

	ext := filepath.Ext(path)
	var results []Results

	switch ext {
	case gator.ExtYAML, gator.ExtYML:
		err = yaml.Unmarshal(data, &results)
	default:
		// Default to JSON
		err = json.Unmarshal(data, &results)
	}
	if err != nil {
		return nil, fmt.Errorf("unmarshaling baseline: %w", err)
	}

	return results, nil
}

// Compare compares current results against baseline results and returns comparison data.
// The threshold is the percentage change considered a regression (e.g., 10 means 10%).
// The minThreshold is the minimum absolute difference to consider a regression.
// For latency metrics, positive change = regression. For throughput, negative change = regression.
func Compare(baseline, current []Results, threshold float64, minThreshold time.Duration) []ComparisonResult {
	var comparisons []ComparisonResult

	// Create a map of baseline results by engine for easy lookup
	baselineByEngine := make(map[Engine]*Results)
	for i := range baseline {
		baselineByEngine[baseline[i].Engine] = &baseline[i]
	}

	// Compare each current result against its baseline
	for i := range current {
		curr := &current[i]
		base, ok := baselineByEngine[curr.Engine]
		if !ok {
			// No baseline for this engine, skip comparison
			continue
		}

		comparison := compareResults(base, curr, threshold, minThreshold)
		comparisons = append(comparisons, comparison)
	}

	return comparisons
}

func compareResults(baseline, current *Results, threshold float64, minThreshold time.Duration) ComparisonResult {
	var metrics []MetricComparison
	var failedMetrics []string
	allPassed := true

	// Compare latency metrics (higher is worse, so positive delta = regression)
	latencyMetrics := []struct {
		name     string
		baseline float64
		current  float64
	}{
		{"P50 Latency", float64(baseline.Latencies.P50), float64(current.Latencies.P50)},
		{"P95 Latency", float64(baseline.Latencies.P95), float64(current.Latencies.P95)},
		{"P99 Latency", float64(baseline.Latencies.P99), float64(current.Latencies.P99)},
		{"Mean Latency", float64(baseline.Latencies.Mean), float64(current.Latencies.Mean)},
	}

	for _, m := range latencyMetrics {
		delta := calculateDelta(m.baseline, m.current)
		// For latency, check both percentage threshold AND minimum absolute threshold
		// If minThreshold is set, ignore regressions smaller than the absolute minimum
		absDiff := time.Duration(m.current) - time.Duration(m.baseline)
		passed := delta <= threshold || (minThreshold > 0 && absDiff < minThreshold)
		if !passed {
			allPassed = false
			failedMetrics = append(failedMetrics, m.name)
		}
		metrics = append(metrics, MetricComparison{
			Name:     m.name,
			Baseline: m.baseline,
			Current:  m.current,
			Delta:    delta,
			Passed:   passed,
		})
	}

	// Compare throughput (lower is worse, so negative delta = regression)
	throughputDelta := calculateDelta(baseline.ReviewsPerSecond, current.ReviewsPerSecond)
	// For throughput, we invert the logic: negative delta is a regression
	throughputPassed := -throughputDelta <= threshold
	if !throughputPassed {
		allPassed = false
		failedMetrics = append(failedMetrics, "Throughput")
	}
	metrics = append(metrics, MetricComparison{
		Name:     "Throughput",
		Baseline: baseline.ReviewsPerSecond,
		Current:  current.ReviewsPerSecond,
		Delta:    throughputDelta,
		Passed:   throughputPassed,
	})

	// Compare memory stats if available
	if baseline.MemoryStats != nil && current.MemoryStats != nil {
		allocsDelta := calculateDelta(
			float64(baseline.MemoryStats.AllocsPerReview),
			float64(current.MemoryStats.AllocsPerReview),
		)
		allocsPassed := allocsDelta <= threshold
		if !allocsPassed {
			allPassed = false
			failedMetrics = append(failedMetrics, "Allocs/Review")
		}
		metrics = append(metrics, MetricComparison{
			Name:     "Allocs/Review",
			Baseline: float64(baseline.MemoryStats.AllocsPerReview),
			Current:  float64(current.MemoryStats.AllocsPerReview),
			Delta:    allocsDelta,
			Passed:   allocsPassed,
		})

		bytesDelta := calculateDelta(
			float64(baseline.MemoryStats.BytesPerReview),
			float64(current.MemoryStats.BytesPerReview),
		)
		bytesPassed := bytesDelta <= threshold
		if !bytesPassed {
			allPassed = false
			failedMetrics = append(failedMetrics, "Bytes/Review")
		}
		metrics = append(metrics, MetricComparison{
			Name:     "Bytes/Review",
			Baseline: float64(baseline.MemoryStats.BytesPerReview),
			Current:  float64(current.MemoryStats.BytesPerReview),
			Delta:    bytesDelta,
			Passed:   bytesPassed,
		})
	}

	return ComparisonResult{
		BaselineEngine: baseline.Engine,
		CurrentEngine:  current.Engine,
		Metrics:        metrics,
		Passed:         allPassed,
		FailedMetrics:  failedMetrics,
	}
}

// calculateDelta calculates the percentage change from baseline to current.
// Returns positive value if current > baseline (regression for latency metrics).
func calculateDelta(baseline, current float64) float64 {
	if baseline == 0 {
		if current == 0 {
			return 0
		}
		return 100 // Infinite increase represented as 100%
	}
	return ((current - baseline) / baseline) * 100
}
