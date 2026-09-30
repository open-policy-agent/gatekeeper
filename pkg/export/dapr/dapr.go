package dapr

import (
	"context"
	"encoding/json"
	"fmt"

	daprClient "github.com/dapr/go-sdk/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
)

type Connection struct {
	// Name of the component object to use in Dapr
	component string

	client daprClient.Client
}

// Dapr represents driver to use Dapr.
type Dapr struct {
	openConnections map[string]Connection
}

const (
	Name = "dapr"
)

var Connections = &Dapr{
	openConnections: make(map[string]Connection),
}

func (r *Dapr) Publish(_ context.Context, connectionName string, data interface{}, topic string) error {
	// Admission violation export is an alpha feature that relies on the durable,
	// bounded on-disk spool implemented only by the disk driver (record-size
	// limits, rotation, retention, and crash recovery). The Dapr driver provides
	// none of those guarantees, so reject admission violations here to fail fast
	// instead of silently publishing them over the message bus.
	if isAdmissionViolation(data) {
		return fmt.Errorf("admission violation export is not supported by the Dapr driver; set the export backend to disk")
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling data: %w", err)
	}

	conn, ok := r.openConnections[connectionName]
	if !ok {
		return fmt.Errorf("connection not found: %s for Dapr driver", connectionName)
	}
	err = conn.client.PublishEvent(context.Background(), conn.component, topic, jsonData)
	if err != nil {
		return fmt.Errorf("error publishing message to dapr: %w", err)
	}

	return nil
}

// isAdmissionViolation reports whether data is an admission violation export
// message. It mirrors the message shapes accepted by the disk driver so the
// guard holds regardless of how the caller passes the payload.
func isAdmissionViolation(data interface{}) bool {
	switch value := data.(type) {
	case util.ExportMsg:
		return value.EventType == util.AdmissionViolationEventType
	case *util.ExportMsg:
		return value != nil && value.EventType == util.AdmissionViolationEventType
	case json.RawMessage:
		var msg util.ExportMsg
		if err := json.Unmarshal(value, &msg); err != nil {
			return false
		}
		return msg.EventType == util.AdmissionViolationEventType
	default:
		return false
	}
}

func (r *Dapr) CloseConnection(connectionName string) error {
	conn, ok := r.openConnections[connectionName]
	if !ok {
		return fmt.Errorf("connection %s not found for disk driver", connectionName)
	}
	defer delete(r.openConnections, connectionName)
	conn.client.Close()
	return nil
}

func (r *Dapr) UpdateConnection(_ context.Context, connectionName string, config interface{}) error {
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	component, ok := cfg["component"].(string)
	if !ok {
		return fmt.Errorf("failed to get value of component")
	}
	conn := r.openConnections[connectionName]
	conn.component = component
	r.openConnections[connectionName] = conn
	return nil
}

func (r *Dapr) CreateConnection(_ context.Context, connectionName string, config interface{}) error {
	var conn Connection
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	conn.component, ok = cfg["component"].(string)
	if !ok {
		return fmt.Errorf("failed to get value of component")
	}

	tmp, err := daprClient.NewClient()
	if err != nil {
		return err
	}

	conn.client = tmp
	r.openConnections[connectionName] = conn
	return nil
}
