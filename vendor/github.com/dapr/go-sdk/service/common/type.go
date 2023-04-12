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

package common

import (
	"encoding/json"
)

// TopicEvent is the content of the inbound topic message.
type TopicEvent struct {
	// ID identifies the event.
	ID string `json:"id"`
	// The version of the CloudEvents specification.
	SpecVersion string `json:"specversion"`
	// The type of event related to the originating occurrence.
	Type string `json:"type"`
	// Source identifies the context in which an event happened.
	Source string `json:"source"`
	// The content type of data value.
	DataContentType string `json:"datacontenttype"`
	// The content of the event.
	// Note, this is why the gRPC and HTTP implementations need separate structs for cloud events.
	Data interface{} `json:"data"`
	// The content of the event represented as raw bytes.
	// This can be deserialized into a Go struct using `Struct`.
	RawData []byte `json:"-"`
	// The base64 encoding content of the event.
	// Note, this is processing rawPayload and binary content types.
	// This field is deprecated and will be removed in the future.
	DataBase64 string `json:"data_base64,omitempty"`
	// Cloud event subject
	Subject string `json:"subject"`
	// The pubsub topic which publisher sent to.
	Topic string `json:"topic"`
	// PubsubName is name of the pub/sub this message came from
	PubsubName string `json:"pubsubname"`
}

func (e *TopicEvent) Struct(target interface{}) error {
	// TODO: Enhance to inspect DataContentType for the best
	// deserialization method.
	return json.Unmarshal(e.RawData, target)
}

// InvocationEvent represents the input and output of binding invocation.
type InvocationEvent struct {
	// Data is the payload that the input bindings sent.
	Data []byte `json:"data"`
	// ContentType of the Data
	ContentType string `json:"contentType"`
	// DataTypeURL is the resource URL that uniquely identifies the type of the serialized
	DataTypeURL string `json:"typeUrl,omitempty"`
	// Verb is the HTTP verb that was used to invoke this service.
	Verb string `json:"-"`
	// QueryString represents an encoded HTTP url query string in the following format: name=value&name2=value2
	QueryString string `json:"-"`
}

// Content is a generic data content.
type Content struct {
	// Data is the payload that the input bindings sent.
	Data []byte `json:"data"`
	// ContentType of the Data
	ContentType string `json:"contentType"`
	// DataTypeURL is the resource URL that uniquely identifies the type of the serialized
	DataTypeURL string `json:"typeUrl,omitempty"`
}

// BindingEvent represents the binding event handler input.
type BindingEvent struct {
	// Data is the input bindings sent
	Data []byte `json:"data"`
	// Metadata is the input binding metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Subscription represents single topic subscription.
type Subscription struct {
	// PubsubName is name of the pub/sub this message came from
	PubsubName string `json:"pubsubname"`
	// Topic is the name of the topic
	Topic string `json:"topic"`
	// Metadata is the subscription metadata
	Metadata map[string]string `json:"metadata,omitempty"`
	// Route is the route of the handler where HTTP topic events should be published (passed as Path in gRPC)
	Route string `json:"route"`
	// Match is the CEL expression to match on the CloudEvent envelope.
	Match string `json:"match"`
	// Priority is the priority in which to evaluate the match (lower to higher).
	Priority int `json:"priority"`
	// DisableTopicValidation allows to receive events from publisher topics that differ from the subscribed topic.
	DisableTopicValidation bool `json:"disableTopicValidation"`
}

const (
	// SubscriptionResponseStatusSuccess means message is processed successfully.
	SubscriptionResponseStatusSuccess = "SUCCESS"
	// SubscriptionResponseStatusRetry means message to be retried by Dapr.
	SubscriptionResponseStatusRetry = "RETRY"
	// SubscriptionResponseStatusDrop means warning is logged and message is dropped.
	SubscriptionResponseStatusDrop = "DROP"
)

// SubscriptionResponse represents the response handling hint from subscriber to Dapr.
type SubscriptionResponse struct {
	Status string `json:"status"`
}
