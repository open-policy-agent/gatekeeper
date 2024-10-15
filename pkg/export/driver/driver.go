package driver

import (
	"context"
)

type Driver interface {
	// Publish single message with specific subject
	Publish(ctx context.Context, connectionName string, data interface{}, subject string) error

	// Close connections
	Close(connectionName string) error

	// Update connection
	Update(ctx context.Context, connectionName string, config interface{}) error

	// Create connection
	Create(ctx context.Context, connectionName string, config interface{}) error
}
