---
id: pubsub-driver
title: Pubsub Interface/Driver walkthrough
---

This guide provides an overview of the pubsub interface, including details on its structure and functionality. Additionally, it offers instructions on adding a new driver and utilizing providers other than the default provider Dapr.

## Pubsub interface and Driver walkthrough

Pubsub's connection interface looks like
```go
// Connection is the interface that wraps pubsub methods.
type Connection interface {
	// Publish single message over a specific topic/channel
	Publish(ctx context.Context, message interface{}, topic string) error

	// Close connections
	CloseConnection() error

	// Update an existing connection with new configuration
	UpdateConnection(ctx context.Context, config interface{}) error
}
```

As an example, the Dapr driver implements these three methods to publish message, close connection, and update connection respectively. Please refer to [dapr.go](https://github.com/open-policy-agent/gatekeeper/blob/master/pkg/pubsub/dapr/dapr.go) to understand the logic that goes in each of these methods. Additionally, the Dapr driver also implements `func NewConnection(_ context.Context, config interface{}) (connection.Connection, error)` method that returns a new client for dapr.

### How to add new drivers

**Note:** For example, if we want to add a new driver to use `foo` instead of Dapr as a tool to publish violations.

A driver must implement the `Connection` interface and a new `func NewConnection(_ context.Context, config interface{}) (connection.Connection, error)` method that returns a client for the respective tool.

This newly added driver's `NewConnection` method must be used to create a new `pubSubs` object in [provider.go](https://github.com/open-policy-agent/gatekeeper/blob/master/pkg/pubsub/provider/provider.go). For example,

```go
var pubSubs = newPubSubSet(map[string]InitiateConnection{
  dapr.Name: dapr.NewConnection,
  "foo": foo.NewConnection,
},
)
```

### How to use different  providers

To enable audit to use this driver to publish messages, a connection configMap with appropriate `config` and `provider` is needed. For example,

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: audit
  namespace: gatekeeper-system
data:
  provider: "foo"
  config: |
    {
      <config needed for foo connection>
    }
```

> The `data.provider` field must exist and must match one of the keys of the `pubSubs` map that was defined earlier to use the corresponding driver. The `data.config` field in the configuration can vary depending on the driver being used. For dapr driver, `data.config` must be `{"component": "pubsub"}`.
