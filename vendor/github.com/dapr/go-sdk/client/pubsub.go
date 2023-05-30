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
	"log"

	"github.com/pkg/errors"

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
				return errors.WithMessage(err, "error serializing input struct")
			}
		}
	}

	_, err := c.protoClient.PublishEvent(c.withAuthToken(ctx), request)
	if err != nil {
		return errors.Wrapf(err, "error publishing event unto %s topic", topicName)
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
		return errors.WithMessage(err, "error serializing input struct")
	}

	return c.PublishEvent(ctx, pubsubName, topicName, enc, PublishEventWithContentType("application/json"))
}
