package common

import (
	"testing"

	testmetric "github.com/open-policy-agent/gatekeeper/v3/test/metrics"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/metric"
)

func TestAddReader(t *testing.T) {
	// Mock the required variables
	rdr := metric.NewPeriodicReader(new(testmetric.FnExporter))

	tests := []struct {
		name            string
		options         []metric.Option
		requiredReaders int
		wantedReaders   int
	}{
		{
			name:            "Only one metrics-backend is available",
			options:         []metric.Option{metric.WithReader(rdr)},
			requiredReaders: 1,
			wantedReaders:   1,
		},
		{
			name:            "More than one metrics-backend is available",
			options:         []metric.Option{metric.WithReader(rdr), metric.WithReader(rdr)},
			requiredReaders: 2,
			wantedReaders:   2,
		},
		{
			name:            "Two metrics-backends are available, but one is in error state",
			options:         []metric.Option{metric.WithReader(rdr), nil},
			requiredReaders: 2,
			wantedReaders:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetRequiredReaders(tt.requiredReaders)
			opts = []metric.Option{}

			for _, opt := range tt.options {
				AddReader(opt)
			}

			assert.Equal(t, tt.wantedReaders, len(opts))
		})
	}
}

func TestSetRequiredReaders(t *testing.T) {
	// Call the function under test
	SetRequiredReaders(5)
	assert.Equal(t, 5, requiredReaders)

	SetRequiredReaders(-1)
	assert.Equal(t, -1, requiredReaders)
}
