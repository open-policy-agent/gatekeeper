/*
Copyright 2021 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"

	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
)

const (
	rawPayload = "rawPayload"
	trueValue  = "true"
)

// PublishEventOption is the type for the functional option.
type PublishEventOption func(*pb.PublishEventRequest)

// PublishEvent publishes data onto specific pubsub topic.
func (c *GRPCClient) PublishEvent(ctx context.Context, pubsubName, topicName string, data interface{}, opts ...PublishEventOption) error {
	if pubsubName == "" {
		return errors.New("pubsubName name required")
	}
	if topicName == "" {
		return errors.New("topic name required")
	}

	request := &pb.PublishEventRequest{
		PubsubName: pubsubName,
		Topic:      topicName,
	}
	for _, o := range opts {
		o(request)
	}

	if data != nil {
		switch d := data.(type) {
		case []byte:
			request.Data = d
		case string:
			request.Data = []byte(d)
		default:
			var err error
			request.DataContentType = "application/json"
			request.Data, err = json.Marshal(d)
			if err != nil {
				return fmt.Errorf("error serializing input struct: %w", err)
			}
		}
	}

	_, err := c.protoClient.PublishEvent(c.withAuthToken(ctx), request)
	if err != nil {
		return fmt.Errorf("error publishing event unto %s topic: %w", topicName, err)
	}

	return nil
}

// PublishEventWithContentType can be passed as option to PublishEvent to set an explicit Content-Type.
func PublishEventWithContentType(contentType string) PublishEventOption {
	return func(e *pb.PublishEventRequest) {
		e.DataContentType = contentType
	}
}

// PublishEventWithMetadata can be passed as option to PublishEvent to set metadata.
func PublishEventWithMetadata(metadata map[string]string) PublishEventOption {
	return func(e *pb.PublishEventRequest) {
		e.Metadata = metadata
	}
}

// PublishEventWithRawPayload can be passed as option to PublishEvent to set rawPayload metadata.
func PublishEventWithRawPayload() PublishEventOption {
	return func(e *pb.PublishEventRequest) {
		if e.Metadata == nil {
			e.Metadata = map[string]string{rawPayload: trueValue}
		} else {
			e.Metadata[rawPayload] = trueValue
		}
	}
}

// PublishEventfromCustomContent serializes an struct and publishes its contents as data (JSON) onto topic in specific pubsub component.
// Deprecated: This method is deprecated and will be removed in a future version of the SDK. Please use `PublishEvent` instead.
func (c *GRPCClient) PublishEventfromCustomContent(ctx context.Context, pubsubName, topicName string, data interface{}) error {
	log.Println("DEPRECATED: client.PublishEventfromCustomContent is deprecated and will be removed in a future version of the SDK. Please use `PublishEvent` instead.")

	// Perform the JSON marshaling here just in case someone passed a []byte or string as data
	enc, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error serializing input struct: %w", err)
	}

	return c.PublishEvent(ctx, pubsubName, topicName, enc, PublishEventWithContentType("application/json"))
}

// PublishEventsEvent is a type of event that can be published using PublishEvents.
type PublishEventsEvent struct {
	EntryID     string
	Data        []byte
	ContentType string
	Metadata    map[string]string
}

// PublishEventsResponse is the response type for PublishEvents.
type PublishEventsResponse struct {
	Error        error
	FailedEvents []interface{}
}

// PublishEventsOption is the type for the functional option.
type PublishEventsOption func(*pb.BulkPublishRequest)

// PublishEvents publishes multiple events onto topic in specific pubsub component.
// If all events are successfully published, response Error will be nil.
// The FailedEvents field will contain all events that failed to publish.
func (c *GRPCClient) PublishEvents(ctx context.Context, pubsubName, topicName string, events []interface{}, opts ...PublishEventsOption) PublishEventsResponse {
	if pubsubName == "" {
		return PublishEventsResponse{
			Error:        errors.New("pubsubName name required"),
			FailedEvents: events,
		}
	}
	if topicName == "" {
		return PublishEventsResponse{
			Error:        errors.New("topic name required"),
			FailedEvents: events,
		}
	}

	failedEvents := make([]interface{}, 0, len(events))
	eventMap := make(map[string]interface{}, len(events))
	entries := make([]*pb.BulkPublishRequestEntry, 0, len(events))
	for _, event := range events {
		entry, err := createBulkPublishRequestEntry(event)
		if err != nil {
			failedEvents = append(failedEvents, event)
			continue
		}
		eventMap[entry.EntryId] = event
		entries = append(entries, entry)
	}

	request := &pb.BulkPublishRequest{
		PubsubName: pubsubName,
		Topic:      topicName,
		Entries:    entries,
	}
	for _, o := range opts {
		o(request)
	}

	res, err := c.protoClient.BulkPublishEventAlpha1(c.withAuthToken(ctx), request)
	// If there is an error, all events failed to publish.
	if err != nil {
		return PublishEventsResponse{
			Error:        fmt.Errorf("error publishing events unto %s topic: %w", topicName, err),
			FailedEvents: events,
		}
	}

	for _, failedEntry := range res.FailedEntries {
		event, ok := eventMap[failedEntry.EntryId]
		if !ok {
			// This should never happen.
			failedEvents = append(failedEvents, failedEntry.EntryId)
		}
		failedEvents = append(failedEvents, event)
	}

	if len(failedEvents) != 0 {
		return PublishEventsResponse{
			Error:        fmt.Errorf("error publishing events unto %s topic: %w", topicName, err),
			FailedEvents: failedEvents,
		}
	}

	return PublishEventsResponse{
		Error:        nil,
		FailedEvents: make([]interface{}, 0),
	}
}

// createBulkPublishRequestEntry creates a BulkPublishRequestEntry from an interface{}.
func createBulkPublishRequestEntry(data interface{}) (*pb.BulkPublishRequestEntry, error) {
	entry := &pb.BulkPublishRequestEntry{}

	switch d := data.(type) {
	case PublishEventsEvent:
		entry.EntryId = d.EntryID
		entry.Event = d.Data
		entry.ContentType = d.ContentType
		entry.Metadata = d.Metadata
	case []byte:
		entry.Event = d
		entry.ContentType = "application/octet-stream"
	case string:
		entry.Event = []byte(d)
		entry.ContentType = "text/plain"
	default:
		var err error
		entry.ContentType = "application/json"
		entry.Event, err = json.Marshal(d)
		if err != nil {
			return &pb.BulkPublishRequestEntry{}, fmt.Errorf("error serializing input struct: %w", err)
		}

		if isCloudEvent(entry.Event) {
			entry.ContentType = "application/cloudevents+json"
		}
	}

	if entry.EntryId == "" {
		entry.EntryId = uuid.New().String()
	}

	return entry, nil
}

// PublishEventsWithContentType can be passed as option to PublishEvents to explicitly set the same Content-Type for all events.
func PublishEventsWithContentType(contentType string) PublishEventsOption {
	return func(r *pb.BulkPublishRequest) {
		for _, entry := range r.Entries {
			entry.ContentType = contentType
		}
	}
}

// PublishEventsWithMetadata can be passed as option to PublishEvents to set request metadata.
func PublishEventsWithMetadata(metadata map[string]string) PublishEventsOption {
	return func(r *pb.BulkPublishRequest) {
		r.Metadata = metadata
	}
}

// PublishEventsWithRawPayload can be passed as option to PublishEvents to set rawPayload request metadata.
func PublishEventsWithRawPayload() PublishEventsOption {
	return func(r *pb.BulkPublishRequest) {
		if r.Metadata == nil {
			r.Metadata = map[string]string{rawPayload: trueValue}
		} else {
			r.Metadata[rawPayload] = trueValue
		}
	}
}
