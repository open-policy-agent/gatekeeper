package pubsub

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
	prvd "github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/provider"
)

type System struct {
	mux         sync.RWMutex
	connections map[string]connection.Connection
	providers   map[string]string
}

func NewSystem() *System {
	return &System{}
}

func (s *System) Publish(ctx context.Context, msg interface{}) error {
	s.mux.RLock()
	defer s.mux.RUnlock()
	var errs error

	if len(s.connections) == 0 {
		return fmt.Errorf("no connections are established")
	}

	for _, c := range s.connections {
		errs = errors.Join(errs, c.Publish(ctx, msg))
	}
	return errs
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
	}
	// Check if the provider is supported.
	if newConnFunc, ok := prvd.List()[provider]; ok {
		newConn, err := newConnFunc(ctx, config)
		if err != nil {
			return err
		}

		// Close the existing connection after successfully creating the new one.
		if err := s.closeConnection(name); err != nil {
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
	return fmt.Errorf("pub-sub provider %s is not supported", provider)
}

func (s *System) CloseConnection(connection string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	return s.closeConnection(connection)
}

func (s *System) closeConnection(connection string) error {
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
