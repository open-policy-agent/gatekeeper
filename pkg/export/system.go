package export

import (
	"context"
	"fmt"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/dapr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/driver"
)

var SupportedDrivers = map[string]driver.Driver{
	dapr.Name: dapr.Connections,
}

type System struct {
	mux         sync.RWMutex
	connections map[string]string
}

func NewSystem() *System {
	return &System{
		connections: map[string]string{},
	}
}

func (s *System) Publish(_ context.Context, connectionName string, subject string, msg interface{}) error {
	s.mux.RLock()
	defer s.mux.RUnlock()
	if c, ok := s.connections[connectionName]; ok {
		return SupportedDrivers[c].Publish(context.Background(), connectionName, msg, subject)
	}
	return fmt.Errorf("connection is not initialized, name: %s ", connectionName)
}

func (s *System) UpsertConnection(ctx context.Context, config interface{}, connectionName string, newDriver string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	// Check if the connection already exists.
	if oldDriver, ok := s.connections[connectionName]; ok {
		// If the provider is the same, update the existing connection.
		if oldDriver == newDriver {
			return SupportedDrivers[newDriver].Update(ctx, connectionName, config)
		}
	}
	// Check if the provider is supported.
	if conn, ok := SupportedDrivers[newDriver]; ok {
		err := conn.Create(ctx, connectionName, config)
		if err != nil {
			return err
		}

		// Close the existing connection after successfully creating the new one.
		if err := s.closeConnection(connectionName); err != nil {
			return err
		}
		// Add the new connection and provider to the maps.
		s.connections[connectionName] = newDriver
		return nil
	}
	return fmt.Errorf("driver %s is not supported", newDriver)
}

func (s *System) CloseConnection(connectionName string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	return s.closeConnection(connectionName)
}

func (s *System) closeConnection(connectionName string) error {
	if c, ok := s.connections[connectionName]; ok {
		if conn, ok := SupportedDrivers[c]; ok {
			err := conn.Close(connectionName)
			if err != nil {
				return err
			}
		}
		delete(s.connections, connectionName)
	}
	return nil
}
