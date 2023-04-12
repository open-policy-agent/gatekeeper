package dapr

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
)

func TestNewConnection(t *testing.T) {
	tests := []struct {
		name     string
		config   interface{}
		expected connection.Connection
		errorMsg string
	}{
		{
			name:     "invalid config",
			config:   "test",
			expected: nil,
			errorMsg: "invalid type assertion, config is not in expected format",
		},
		{
			name:     "config with missing component",
			config:   map[string]interface{}{"enableBatching": true},
			expected: nil,
			errorMsg: "failed to get value of component",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ret, err := NewConnection(context.TODO(), tc.config)
			assert.Equal(t, ret, tc.expected)
			assert.EqualError(t, err, tc.errorMsg)
		})
	}
}
