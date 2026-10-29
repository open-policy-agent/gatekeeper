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

As an example, the disk driver implements these methods to publish messages to files and manage connections. Please refer to [disk.go](https://github.com/open-policy-agent/gatekeeper/blob/master/pkg/export/disk/disk.go) to understand the logic that goes in each of these methods. The disk driver supports both audit and admission export; see [Exporting violations](export.md) for configuration and limitations.

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

This newly added driver's `Connections` exported variable must be added to the map of `supportedDrivers` in [system.go](https://github.com/open-policy-agent/gatekeeper/blob/master/pkg/export/system.go). For example,

```go
var supportedDrivers = map[string]driver.Driver{
	disk.Name: disk.Connections,
  foo.Name: foo.Connections,
}
```

And thats it! Exporter system will take the newly added driver into account and a `Connection` custom resource to establish connection to export message is created.

### How to establish connections to different backend

To enable audit to publish messages through a driver, a `Connection` custom resource with appropriate `config` and `driver` is needed. For example, the built-in disk driver uses:

```yaml
apiVersion: connection.gatekeeper.sh/v1alpha1
kind: Connection
metadata:
  name: audit-connection
  namespace: gatekeeper-system
spec:
  driver: "disk"
  config:
    path: "/tmp/violations/topics"
    maxAuditResults: 3
```

> The `spec.driver` field must exist and must match one of the keys of the `supportedDrivers` map that was defined earlier to use the corresponding driver. The `spec.config` field varies by driver. For disk, `path` must be on a writable mounted volume and `maxAuditResults` controls audit retention. A newly registered `foo` driver would use `spec.driver: foo` and its own configuration. See [Exporting violations](export.md) for per-pod storage and consumer requirements.
