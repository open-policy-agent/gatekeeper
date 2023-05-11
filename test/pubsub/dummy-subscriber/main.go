package main

import (
	"context"
	"encoding/json"
	"log"
	"strconv"

	"github.com/dapr/go-sdk/service/common"
	daprd "github.com/dapr/go-sdk/service/http"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/audit"
)

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
	var msg audit.PubsubMsg
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
