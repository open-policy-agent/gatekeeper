---
id: pubsub-driver
title: Pubsub Interface/Driver walk through
---

This guide provides an overview of the pubsub interface, including details on its structure and functionality. Additionally, it offers instructions on adding a new driver and utilizing providers other than Dapr.

## Pubsub interface and Driver walk through

Pubsub's connection interface looks like
```
// PubSub is the interface that wraps pubsub methods.
type Connection interface {
	// Publish single message over a specific topic/channel
	Publish(ctx context.Context, data interface{}, topic string) error

	// Close connections
	CloseConnection() error

	// Update connection
	UpdateConnection(ctx context.Context, data interface{}) error
}
```

Dapr driver implements these three methods to publish message, close connection, and update connection respectively. Please refer to [dapr.go](../../pkg/pubsub/dapr/dapr.go) to understand the logic that goes in each of these methods. Additionally, Dapr driver also implements a method `func NewConnection(_ context.Context, config interface{}) (connection.Connection, error)` that returns a new client for dapr.

### How to add new drivers

**Note:** For this exercise, let's say we are trying to add a driver to use `RabbitMQ` instead of Dapr as a tool to publish violations.

Any new driver has to implement the methods of `Connection` interface and additionally a new method  `func NewConnection(_ context.Context, config interface{}) (connection.Connection, error)` that returns client for respective tool.

> The name of the method that returns client could change, but the signature of the method must be the same.

This newly added driver's `NewConnection` method has to be added in `pubSubs` variable in [provider.go](../../pkg/pubsub/provider/provider.go). For example,

```
var pubSubs = newPubSubSet(map[string]InitiateConnection{
	dapr.Name: dapr.NewConnection,
    rabbitmq.Name: rabbitmq.NewConnection,
},
)
```

### How to use different providers

To enable audit to use this driver to publish messages, a connection configMap with appropriate `config` and `provider` is needed. For example,

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: audit
  namespace: gatekeeper-system
data:
  provider: "rabbitmq"
  config: |
    {
      <config needed for rabbitmq connection>
    }
```