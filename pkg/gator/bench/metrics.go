package bench

import (
	"sort"
	"time"
)

// calculateLatencies computes latency statistics from a slice of durations.
func calculateLatencies(durations []time.Duration) Latencies {
	if len(durations) == 0 {
		return Latencies{}
	}

	// Sort for percentile calculation
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	var total time.Duration
	for _, d := range sorted {
		total += d
	}

	return Latencies{
		Min:  sorted[0],
		Max:  sorted[len(sorted)-1],
		Mean: time.Duration(int64(total) / int64(len(sorted))),
		P50:  percentile(sorted, 50),
		P95:  percentile(sorted, 95),
		P99:  percentile(sorted, 99),
	}
}

// percentile calculates the p-th percentile from a sorted slice of durations.
// The input slice must be sorted in ascending order.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	// Calculate the index using the nearest-rank method
	rank := (p / 100.0) * float64(len(sorted)-1)
	lower := int(rank)
	upper := lower + 1

	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	// Linear interpolation between the two nearest ranks
	weight := rank - float64(lower)
	return time.Duration(float64(sorted[lower])*(1-weight) + float64(sorted[upper])*weight)
}

// calculateThroughput computes reviews per second.
func calculateThroughput(reviewCount int, duration time.Duration) float64 {
	if duration == 0 {
		return 0
	}
	return float64(reviewCount) / duration.Seconds()
}
