package dapr

import (
	"context"
	"os"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/driver"
	"github.com/stretchr/testify/assert"
)

var testClient driver.Driver

func TestMain(m *testing.M) {
	c, f := FakeConnection()
	testClient = c
	r := m.Run()
	f()

	if r != 0 {
		os.Exit(r)
	}
}

func TestCreate(t *testing.T) {
	tests := []struct {
		name                string
		config              interface{}
		expectedConnections int
		errorMsg            string
	}{
		{
			name:                "invalid config",
			config:              "test",
			expectedConnections: 1,
			errorMsg:            "invalid type assertion, config is not in expected format",
		},
		{
			name:                "config with missing component",
			config:              map[string]interface{}{"enableBatching": true},
			expectedConnections: 1,
			errorMsg:            "failed to get value of component",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := testClient.CreateConnection(context.TODO(), "another-test", tc.config)
			tmp, ok := testClient.(*Dapr)
			if !ok {
				t.Errorf("failed to type assert")
			}
			assert.Equal(t, tc.expectedConnections, len(tmp.openConnections))
			assert.EqualError(t, err, tc.errorMsg)
		})
	}
}

func TestDapr_Publish(t *testing.T) {
	ctx := context.Background()

	type args struct {
		ctx            context.Context
		data           interface{}
		topic          string
		connectionName string
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
				topic:          "test",
				connectionName: "test",
			},
			wantErr: false,
		},
		{
			name: "test publish without data",
			args: args{
				ctx:            ctx,
				data:           nil,
				topic:          "test",
				connectionName: "test",
			},
			wantErr: false,
		},
		{
			name: "test publish without topic",
			args: args{
				ctx: ctx,
				data: map[string]interface{}{
					"test": "test",
				},
				topic:          "",
				connectionName: "test",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testClient
			if err := r.Publish(tt.args.ctx, tt.args.connectionName, tt.args.data, tt.args.topic); (err != nil) != tt.wantErr {
				t.Errorf("Dapr.Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDapr_Update(t *testing.T) {
	tests := []struct {
		name           string
		config         interface{}
		connectionName string
		wantErr        bool
	}{
		{
			name: "test update connection",
			config: map[string]interface{}{
				"component": "foo",
			},
			wantErr:        false,
			connectionName: "test",
		},
		{
			name: "test update connection with invalid config",
			config: map[string]interface{}{
				"foo": "bar",
			},
			connectionName: "test",
			wantErr:        true,
		},
		{
			name:           "test update connection with nil config",
			config:         nil,
			connectionName: "test",
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testClient
			if err := r.UpdateConnection(context.Background(), tt.connectionName, tt.config); (err != nil) != tt.wantErr {
				t.Errorf("Dapr.Update() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				cmp, ok := tt.config.(map[string]interface{})["component"].(string)
				assert.True(t, ok)
				tmp, ok := r.(*Dapr)
				assert.True(t, ok)
				assert.Equal(t, cmp, tmp.openConnections[tt.connectionName].component)
			}
		})
	}
}
