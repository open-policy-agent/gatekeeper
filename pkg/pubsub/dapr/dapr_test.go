package dapr

import (
	"context"
	"os"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
	"github.com/stretchr/testify/assert"
)

var testClient connection.Connection

func TestMain(m *testing.M) {
	c, f := FakeConnection()
	testClient = c
	r := m.Run()
	f()

	if r != 0 {
		os.Exit(r)
	}
}

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

func TestDapr_Publish(t *testing.T) {
	ctx := context.Background()

	type args struct {
		ctx  context.Context
		data interface{}
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "test publish",
			args: args{
				ctx: ctx,
				data: map[string]interface{}{
					"test": "test",
				},
			},
			wantErr: false,
		},
		{
			name: "test publish without data",
			args: args{
				ctx:  ctx,
				data: nil,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testClient
			if err := r.Publish(tt.args.ctx, tt.args.data); (err != nil) != tt.wantErr {
				t.Errorf("Dapr.Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDapr_UpdateConnection(t *testing.T) {
	tests := []struct {
		name    string
		config  interface{}
		wantErr bool
	}{
		{
			name: "test update connection",
			config: map[string]interface{}{
				"component": "foo",
			},
			wantErr: false,
		},
		{
			name: "test update connection with invalid config",
			config: map[string]interface{}{
				"foo": "bar",
			},
			wantErr: true,
		},
		{
			name:    "test update connection with nil config",
			config:  nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testClient
			if err := r.UpdateConnection(context.Background(), tt.config); (err != nil) != tt.wantErr {
				t.Errorf("Dapr.UpdateConnection() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				cmp, ok := tt.config.(map[string]interface{})["component"].(string)
				assert.True(t, ok)
				tmp, ok := r.(*Dapr)
				assert.True(t, ok)
				assert.Equal(t, cmp, tmp.pubSubComponent)
			}
		})
	}
}
