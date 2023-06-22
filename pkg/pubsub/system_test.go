package pubsub

import (
	"context"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/dapr"
	"github.com/stretchr/testify/assert"
)

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
	}{
		{
			name: "Updating connections for unsupported provider",
			fields: fields{
				connections: map[string]connection.Connection{},
				providers:   map[string]string{"audit": "dapr"},
			},
			args: args{
				ctx:      context.Background(),
				config:   nil,
				name:     "audit",
				provider: "test",
			},
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
			if err := s.UpsertConnection(tt.args.ctx, tt.args.config, tt.args.name, tt.args.provider); (err != nil) != tt.wantErr {
				t.Errorf("System.UpsertConnection() error = %v, wantErr %v", err, tt.wantErr)
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
