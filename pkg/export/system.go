package export

import (
	"context"
	"fmt"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/dapr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/driver"
)

var supportedDrivers = map[string]driver.Driver{
	dapr.Name: dapr.Connections,
	disk.Name: disk.Connections,
}

type Exporter interface {
	Publish(ctx context.Context, connectionName string, subject string, msg interface{}) error
	UpsertConnection(ctx context.Context, config interface{}, connectionName string, newDriver string) error
	CloseConnection(connectionName string) error
}

// BatchExporter is an optional extension for bounded batches. Returned errors
// correspond one-to-one with messages in input order. Callers fall back to
// Exporter.Publish when it is unavailable.
type BatchExporter interface {
	PublishBatch(ctx context.Context, connectionName string, subject string, messages []any) []error
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

func (s *System) Publish(ctx context.Context, connectionName string, subject string, msg interface{}) error {
	s.mux.RLock()
	defer s.mux.RUnlock()
	if dName, ok := s.connectionToDriver[connectionName]; ok {
		return supportedDrivers[dName].Publish(ctx, connectionName, msg, subject)
	}
	return fmt.Errorf("connection is not initialized, name: %s ", connectionName)
}

func (s *System) PublishBatch(ctx context.Context, connectionName string, subject string, messages []any) []error {
	s.mux.RLock()
	defer s.mux.RUnlock()
	driverName, ok := s.connectionToDriver[connectionName]
	if !ok {
		return batchErrors(len(messages), fmt.Errorf("connection is not initialized, name: %s ", connectionName))
	}
	connectionDriver := supportedDrivers[driverName]
	if batchDriver, ok := connectionDriver.(driver.BatchDriver); ok {
		return batchDriver.PublishBatch(ctx, connectionName, messages, subject)
	}
	errorsByMessage := make([]error, len(messages))
	for i := range messages {
		errorsByMessage[i] = connectionDriver.Publish(ctx, connectionName, messages[i], subject)
	}
	return errorsByMessage
}

func batchErrors(count int, err error) []error {
	errorsByMessage := make([]error, count)
	for i := range errorsByMessage {
		errorsByMessage[i] = err
	}
	return errorsByMessage
}

func (s *System) UpsertConnection(ctx context.Context, config interface{}, connectionName string, newDriver string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	// Check if the connection already exists.
	if oldDriver, ok := s.connectionToDriver[connectionName]; ok {
		// If the provider is the same, update the existing connection.
		if oldDriver == newDriver {
			return supportedDrivers[newDriver].UpdateConnection(ctx, connectionName, config)
		}
	}
	// Check if the provider is supported.
	if d, ok := supportedDrivers[newDriver]; ok {
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
	if driverName, ok := s.connectionToDriver[connectionName]; ok {
		// connection should be deleted from the map before closing it to make sure old connection is not accessible if close fails
		// also avoids not respecting the latest connection with the same name if close fails
		delete(s.connectionToDriver, connectionName)
		if conn, ok := supportedDrivers[driverName]; ok {
			err := conn.CloseConnection(connectionName)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
