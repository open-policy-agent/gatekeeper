package dapr

import (
	"context"
	"encoding/json"
	"fmt"

	daprClient "github.com/dapr/go-sdk/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
)

type ClientConfig struct {
	// Name of the component to be used for pub sub messaging
	Component string `json:"component"`
}

// Dapr represents driver for interacting with pub sub using dapr.
type Dapr struct {
	// Array of clients to talk to different endpoints
	client daprClient.Client

	// Name of the pubsub component
	pubSubComponent string
}

const (
	Name = "dapr"
)

func (r *Dapr) Publish(ctx context.Context, data interface{}, topic string) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling data: %w", err)
	}

	err = r.client.PublishEvent(context.Background(), r.pubSubComponent, topic, jsonData)
	if err != nil {
		return fmt.Errorf("error publishing message to dapr: %w", err)
	}

	return nil
}

func (r *Dapr) CloseConnection() error {
	return nil
}

func (r *Dapr) UpdateConnection(ctx context.Context, config interface{}) error {
	var cfg ClientConfig
	m, ok := config.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	cfg.Component, ok = m["component"].(string)
	if !ok {
		return fmt.Errorf("failed to get value of component")
	}
	r.pubSubComponent = cfg.Component
	return nil
}

// Returns a new client for dapr.
func NewConnection(_ context.Context, config interface{}) (connection.Connection, error) {
	var cfg ClientConfig
	m, ok := config.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	cfg.Component, ok = m["component"].(string)
	if !ok {
		return nil, fmt.Errorf("failed to get value of component")
	}

	tmp, err := daprClient.NewClient()
	if err != nil {
		return nil, err
	}

	return &Dapr{
		client:          tmp,
		pubSubComponent: cfg.Component,
	}, nil
}
