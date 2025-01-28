package driver

import (
	"context"
)

type Driver interface {
	// Publish publishes single message with specific subject using a connection
	Publish(ctx context.Context, connectionName string, data interface{}, subject string) error

	// CloseConnection closes a connection
	CloseConnection(connectionName string) error

	// UpdateConnection updates an existing connection
	UpdateConnection(ctx context.Context, connectionName string, config interface{}) error

	// CreateConnection creates new connection
	CreateConnection(ctx context.Context, connectionName string, config interface{}) error
}
