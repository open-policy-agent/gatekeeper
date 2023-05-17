package pubsub

import (
	"context"
	"fmt"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
	prvd "github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/provider"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("pubsub-system")

type System struct {
	mux         sync.RWMutex
	connections map[string]connection.Connection
	providers   map[string]string
}

func NewSystem() *System {
	return &System{}
}

func (s *System) Publish(ctx context.Context, connection string, topic string, msg interface{}) error {
	s.mux.RLock()
	defer s.mux.RUnlock()
	if len(s.connections) > 0 {
		if c, ok := s.connections[connection]; ok {
			return c.Publish(context.Background(), msg, topic)
		}
		return fmt.Errorf("connection is not initialized, name: %s ", connection)
	}
	return fmt.Errorf("No connections are established")
}

func (s *System) UpsertConnection(ctx context.Context, config interface{}, name string, provider string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	// Check if the connection already exists.
	if conn, ok := s.connections[name]; ok {
		// If the provider is the same, update the existing connection.
		if s.providers[name] == provider {
			return conn.UpdateConnection(ctx, config)
		}
		// Otherwise, close the existing connection and create a new one.
		if err := s.CloseConnection(name); err != nil {
			return err
		}
	}
	// Check if the provider is supported.
	if newConnFunc, ok := prvd.List()[provider]; ok {
		newConn, err := newConnFunc(ctx, config)
		if err != nil {
			return err
		}
		// Add the new connection and provider to the maps.
		if s.connections == nil {
			s.connections = map[string]connection.Connection{}
		}
		if s.providers == nil {
			s.providers = map[string]string{}
		}
		s.connections[name] = newConn
		s.providers[name] = provider
		return nil
	}
	log.Info(fmt.Sprintf("Pub-sub provider %s is not supported", provider))
	return nil
}

func (s *System) CloseConnection(connection string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	if len(s.connections) > 0 {
		if c, ok := s.connections[connection]; ok {
			err := c.CloseConnection()
			if err != nil {
				return err
			}
			delete(s.connections, connection)
			delete(s.providers, connection)
		}
	}
	return nil
}
