---
id: export-driver
title: Export Interface/Driver walkthrough
---

This guide provides an overview of the driver interface, including details of its structure and functionality. Additionally, it offers instructions on adding a new driver and utilizing different backends to export audit violations.

## Driver interface

```go
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
```

As an example, the Dapr driver implements these methods to publish message and manage connection to do so. Please refer to [dapr.go](https://github.com/open-policy-agent/gatekeeper/blob/master/pkg/export/dapr/dapr.go) to understand the logic that goes in each of these methods.

### How to add new driver to export audit violations to foo backend

A driver must maintain a map of open connections associated with backend `foo`.

```go
type Connection struct {
	// properties needed for individual connection
}

type Foo struct {
	openConnections map[string]Connection
}

const (
	Name = "foo"
)

var Connections = &Foo{
	openConnections: make(map[string]Connection),
}

```

A driver must implement the `Driver` interface.

```go
func (r *Foo) Publish(ctx context.Context, connectionName string, data interface{}, subject string) error {
  ...
}

func (r *Foo) CloseConnection(connectionName string) error {
  ...
}

func (r *Foo) UpdateConnection(ctx context.Context, connectionName string, config interface{}) error {
  ...
}

func (r *Foo) CreateConnection(ctx context.Context, connectionName string, config interface{}) error {
  ...
}
```

This newly added driver's `Connections` exported variable must be added to the map of `SupportedDrivers` in [system.go](https://github.com/open-policy-agent/gatekeeper/blob/master/pkg/export/provider/system.go). For example,

```go
var SupportedDrivers = map[string]driver.Driver{
	dapr.Name: dapr.Connections,
  foo.Name: foo.Connections,
}
```

And thats it! Exporter system will take the newly added driver into account and a `Connection` custom resource to establish connection to export message is created.

### How to establish connections to different backend

To enable audit to use this driver to publish messages, a `Connection` custom resource with appropriate `config` and `driver` is needed. For example,

```yaml
apiVersion: connection.gatekeeper.sh/v1alpha1
kind: Connection
metadata:
  name: audit-connection
  namespace: gatekeeper-system
spec:
  driver: "foo"
  config:
    key: value
```

> The `data.driver` field must exist and must match one of the keys of the `SupportedDrivers` map that was defined earlier to use the corresponding driver. The `data.config` field in the configuration can vary depending on the driver being used. For dapr driver, `data.config` must be `component: "pubsub"`.
