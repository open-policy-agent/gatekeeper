package pubsub

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/dapr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/provider"
	"github.com/stretchr/testify/assert"
)

var testSystem *System

func TestMain(m *testing.M) {
	ctx := context.Background()
	provider.FakeProviders()
	tmp := provider.List()
	testSystem = NewSystem()
	testSystem.connections = make(map[string]connection.Connection)
	testSystem.providers = make(map[string]string)
	cfg := map[string]interface{}{
		dapr.Name: map[string]interface{}{
			"component": "pubsub",
		},
	}
	for name, fakeConn := range tmp {
		testSystem.providers[name] = name
		testSystem.connections[name], _ = fakeConn(ctx, cfg[name])
	}
	r := m.Run()
	for _, fakeConn := range testSystem.connections {
		_ = fakeConn.CloseConnection()
	}

	if r != 0 {
		os.Exit(r)
	}
}

func TestNewSystem(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  *System
	}{
		{
			name: "requesting system",
			want: &System{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ret := NewSystem()
			assert.Equal(t, ret, tc.want)
		})
	}
}

func TestSystem_UpsertConnection(t *testing.T) {
	type fields struct {
		connections map[string]connection.Connection
		providers   map[string]string
		s           *System
	}
	type args struct {
		ctx      context.Context
		config   interface{}
		name     string
		provider string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
		match   bool
	}{
		{
			name: "Create a new connection with dapr provider",
			fields: fields{
				connections: testSystem.connections,
				providers:   testSystem.providers,
				s:           &System{},
			},
			args: args{
				ctx: context.Background(),
				config: map[string]interface{}{
					"component": "pubsub",
				},
				name:     "dapr",
				provider: "dapr",
			},
			wantErr: false,
			match:   true,
		},
		{
			name: "Update a connection to use test provider",
			fields: fields{
				connections: nil,
				providers:   map[string]string{"audit": "dapr"},
				s: &System{
					mux:       sync.RWMutex{},
					providers: map[string]string{"audit": "dapr"},
				},
			},
			args: args{
				ctx: context.Background(),
				config: map[string]interface{}{
					"component": "pubsub",
				},
				name:     "audit",
				provider: "test",
			},
			wantErr: true,
			match:   true,
		},
		{
			name: "Update a connection using same provider",
			fields: fields{
				connections: testSystem.connections,
				providers:   map[string]string{"dapr": "dapr"},
				s: &System{
					mux:         sync.RWMutex{},
					providers:   testSystem.providers,
					connections: testSystem.connections,
				},
			},
			args: args{
				ctx: context.Background(),
				config: map[string]interface{}{
					"component": "test",
				},
				name:     "audit",
				provider: "dapr",
			},
			wantErr: false,
			match:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fields.s.UpsertConnection(tt.args.ctx, tt.args.config, tt.args.name, tt.args.provider); (err != nil) != tt.wantErr {
				t.Errorf("System.UpsertConnection() error = %v, wantErr %v", err, tt.wantErr)
			}
			assert.NotEqual(t, nil, tt.fields.s.connections)
			if tt.match {
				assert.Equal(t, tt.fields.providers, tt.fields.s.providers)
			} else {
				assert.NotEqual(t, tt.fields.providers, tt.fields.s.providers)
			}
		})
	}
}

func TestSystem_CloseConnection(t *testing.T) {
	type fields struct {
		connections map[string]connection.Connection
		providers   map[string]string
	}
	type args struct {
		connection string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "closing connection",
			fields: fields{
				connections: map[string]connection.Connection{"audit": &dapr.Dapr{}},
				providers:   map[string]string{"audit": "dapr"},
			},
			args:    args{connection: "audit"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &System{
				mux:         sync.RWMutex{},
				connections: tt.fields.connections,
				providers:   tt.fields.providers,
			}
			if err := s.CloseConnection(tt.args.connection); (err != nil) != tt.wantErr {
				t.Errorf("System.CloseConnection() error = %v, wantErr %v", err, tt.wantErr)
				_, ok := s.connections[tt.args.connection]
				assert.False(t, ok)
			}
		})
	}
}

func TestSystem_Publish(t *testing.T) {
	type fields struct {
		connections map[string]connection.Connection
		providers   map[string]string
	}
	type args struct {
		ctx        context.Context
		connection string
		topic      string
		msg        interface{}
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "There are no connections established",
			fields: fields{
				connections: nil,
				providers:   nil,
			},
			args:    args{ctx: context.Background(), connection: "audit", topic: "test", msg: nil},
			wantErr: true,
		},
		{
			name: "Publishing to a connection that does not exist",
			fields: fields{
				connections: map[string]connection.Connection{"audit": &dapr.Dapr{}},
				providers:   map[string]string{"audit": "dapr"},
			},
			args:    args{ctx: context.Background(), connection: "test", topic: "test", msg: nil},
			wantErr: true,
		},
		{
			name: "Publishing to a connection that does exist",
			fields: fields{
				connections: testSystem.connections,
				providers:   testSystem.providers,
			},
			args:    args{ctx: context.Background(), connection: "dapr", topic: "test", msg: nil},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &System{
				mux:         sync.RWMutex{},
				connections: tt.fields.connections,
				providers:   tt.fields.providers,
			}
			if err := s.Publish(tt.args.ctx, tt.args.connection, tt.args.topic, tt.args.msg); (err != nil) != tt.wantErr {
				t.Errorf("System.Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
