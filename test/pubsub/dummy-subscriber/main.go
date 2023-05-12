package main

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/http"
)

type PubsubMsg struct {
	ID                    string            `json:"id,omitempty"`
	Details               interface{}       `json:"details,omitempty"`
	EventType             string            `json:"eventType,omitempty"`
	Group                 string            `json:"group,omitempty"`
	Version               string            `json:"version,omitempty"`
	Kind                  string            `json:"kind,omitempty"`
	Name                  string            `json:"name,omitempty"`
	Namespace             string            `json:"namespace,omitempty"`
	Message               string            `json:"message,omitempty"`
	EnforcementAction     string            `json:"enforcementAction,omitempty"`
	ConstraintAnnotations map[string]string `json:"constraintAnnotations,omitempty"`
	ResourceGroup         string            `json:"resourceGroup,omitempty"`
	ResourceAPIVersion    string            `json:"resourceAPIVersion,omitempty"`
	ResourceKind          string            `json:"resourceKind,omitempty"`
	ResourceNamespace     string            `json:"resourceNamespace,omitempty"`
	ResourceName          string            `json:"resourceName,omitempty"`
	ResourceLabels        map[string]string `json:"resourceLabels,omitempty"`
}

var sub = &common.Subscription{
	PubsubName: "pubsub",
	Topic:      "audit",
	Route:      "/checkout",
}

func main() {
	s := daprd.NewService(":6002")
	log.Printf("Listening...")
	if err := s.AddTopicEventHandler(sub, eventHandler); err != nil {
		log.Fatalf("error adding topic subscription: %v", err)
	}
	if err := s.Start(); err != nil {
		log.Fatalf("error listening: %v", err)
	}
}

func eventHandler(_ context.Context, e *common.TopicEvent) (retry bool, err error) {
	var msg PubsubMsg
	jsonInput, err := strconv.Unquote(string(e.RawData))
	if err != nil {
		log.Fatalf("error unquoting %v", err)
	}
	if err := json.Unmarshal([]byte(jsonInput), &msg); err != nil {
		log.Fatalf("error %v", err)
	}

	log.Printf("%#v", msg)
	return false, nil
}
