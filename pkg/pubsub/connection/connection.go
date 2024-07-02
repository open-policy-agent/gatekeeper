package connection

import (
	"context"
)

// PubSub is the interface that wraps pubsub methods.
type Connection interface {
	// Publish single message over a specific topic/channel
	Publish(ctx context.Context, data interface{}) error

	// Close connections
	CloseConnection() error

	// Update connection
	UpdateConnection(ctx context.Context, data interface{}) error
}
