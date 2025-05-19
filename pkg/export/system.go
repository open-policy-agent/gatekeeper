package export

import (
	"context"
	"fmt"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/dapr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/driver"
)

var SupportedDrivers = map[string]driver.Driver{
	dapr.Name: dapr.Connections,
	disk.Name: disk.Connections,
}

type System struct {
	mux                sync.RWMutex
	connectionToDriver map[string]string
}

func NewSystem() *System {
	return &System{
		connectionToDriver: map[string]string{},
	}
}

func (s *System) Publish(_ context.Context, connectionName string, subject string, msg interface{}) error {
	s.mux.RLock()
	defer s.mux.RUnlock()
	if dName, ok := s.connectionToDriver[connectionName]; ok {
		return SupportedDrivers[dName].Publish(context.Background(), connectionName, msg, subject)
	}
	return fmt.Errorf("connection is not initialized, name: %s ", connectionName)
}

func (s *System) UpsertConnection(ctx context.Context, config interface{}, connectionName string, newDriver string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	// Check if the connection already exists.
	if oldDriver, ok := s.connectionToDriver[connectionName]; ok {
		// If the provider is the same, update the existing connection.
		if oldDriver == newDriver {
			return SupportedDrivers[newDriver].UpdateConnection(ctx, connectionName, config)
		}
	}
	// Check if the provider is supported.
	if d, ok := SupportedDrivers[newDriver]; ok {
		err := d.CreateConnection(ctx, connectionName, config)
		if err != nil {
			return err
		}

		// Close the existing connection after successfully creating the new one.
		if err := s.closeConnection(connectionName); err != nil {
			return err
		}
		// Add the new connection and provider to the maps.
		s.connectionToDriver[connectionName] = newDriver
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
	if c, ok := s.connectionToDriver[connectionName]; ok {
		if conn, ok := SupportedDrivers[c]; ok {
			err := conn.CloseConnection(connectionName)
			if err != nil {
				return err
			}
		}
		delete(s.connectionToDriver, connectionName)
	}
	return nil
}
