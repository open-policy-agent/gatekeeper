package bench

import (
	"testing"
	"time"
)

func TestCalculateLatencies(t *testing.T) {
	tests := []struct {
		name      string
		durations []time.Duration
		wantMin   time.Duration
		wantMax   time.Duration
		wantMean  time.Duration
	}{
		{
			name:      "empty slice",
			durations: []time.Duration{},
			wantMin:   0,
			wantMax:   0,
			wantMean:  0,
		},
		{
			name:      "single duration",
			durations: []time.Duration{100 * time.Millisecond},
			wantMin:   100 * time.Millisecond,
			wantMax:   100 * time.Millisecond,
			wantMean:  100 * time.Millisecond,
		},
		{
			name: "multiple durations",
			durations: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				30 * time.Millisecond,
				40 * time.Millisecond,
				50 * time.Millisecond,
			},
			wantMin:  10 * time.Millisecond,
			wantMax:  50 * time.Millisecond,
			wantMean: 30 * time.Millisecond,
		},
		{
			name: "unsorted durations",
			durations: []time.Duration{
				50 * time.Millisecond,
				10 * time.Millisecond,
				30 * time.Millisecond,
				20 * time.Millisecond,
				40 * time.Millisecond,
			},
			wantMin:  10 * time.Millisecond,
			wantMax:  50 * time.Millisecond,
			wantMean: 30 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateLatencies(tt.durations)

			if got.Min != tt.wantMin {
				t.Errorf("Min = %v, want %v", got.Min, tt.wantMin)
			}
			if got.Max != tt.wantMax {
				t.Errorf("Max = %v, want %v", got.Max, tt.wantMax)
			}
			if got.Mean != tt.wantMean {
				t.Errorf("Mean = %v, want %v", got.Mean, tt.wantMean)
			}
		})
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name   string
		sorted []time.Duration
		p      float64
		want   time.Duration
	}{
		{
			name:   "empty slice",
			sorted: []time.Duration{},
			p:      50,
			want:   0,
		},
		{
			name:   "single element p50",
			sorted: []time.Duration{100 * time.Millisecond},
			p:      50,
			want:   100 * time.Millisecond,
		},
		{
			name: "p50 odd count",
			sorted: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				30 * time.Millisecond,
				40 * time.Millisecond,
				50 * time.Millisecond,
			},
			p:    50,
			want: 30 * time.Millisecond,
		},
		{
			name: "p99 many elements",
			sorted: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				30 * time.Millisecond,
				40 * time.Millisecond,
				50 * time.Millisecond,
			},
			p:    99,
			want: 49600 * time.Microsecond, // interpolated
		},
		{
			name: "p100 returns last element",
			sorted: []time.Duration{
				10 * time.Millisecond,
				20 * time.Millisecond,
				30 * time.Millisecond,
			},
			p:    100,
			want: 30 * time.Millisecond, // upper >= len case
		},
		{
			name:   "two elements p0",
			sorted: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			p:      0,
			want:   10 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.sorted, tt.p)
			// Allow 1ms tolerance for interpolation
			diff := got - tt.want
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Millisecond {
				t.Errorf("percentile(%v, %v) = %v, want %v", tt.sorted, tt.p, got, tt.want)
			}
		})
	}
}

func TestCalculateThroughput(t *testing.T) {
	tests := []struct {
		name        string
		reviewCount int
		duration    time.Duration
		want        float64
	}{
		{
			name:        "zero duration",
			reviewCount: 100,
			duration:    0,
			want:        0,
		},
		{
			name:        "1 second duration",
			reviewCount: 100,
			duration:    time.Second,
			want:        100,
		},
		{
			name:        "500ms duration",
			reviewCount: 50,
			duration:    500 * time.Millisecond,
			want:        100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateThroughput(tt.reviewCount, tt.duration)
			if got != tt.want {
				t.Errorf("calculateThroughput(%v, %v) = %v, want %v",
					tt.reviewCount, tt.duration, got, tt.want)
			}
		})
	}
}
