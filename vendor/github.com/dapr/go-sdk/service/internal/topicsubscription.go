package internal

import (
	"errors"
	"fmt"
	"sort"
)

// TopicSubscription internally represents single topic subscription.
type TopicSubscription struct {
	// PubsubName is name of the pub/sub this message came from.
	PubsubName string `json:"pubsubname"`
	// Topic is the name of the topic.
	Topic string `json:"topic"`
	// Route is the route of the handler where HTTP topic events should be published (passed as Path in gRPC).
	Route string `json:"route,omitempty"`
	// Routes specify multiple routes where topic events should be sent.
	Routes *TopicRoutes `json:"routes,omitempty"`
	// Metadata is the subscription metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// TopicRoutes encapsulates the default route and multiple routing rules.
type TopicRoutes struct {
	Rules   []TopicRule `json:"rules,omitempty"`
	Default string      `json:"default,omitempty"`

	// priority is used to track duplicate priorities where priority > 0.
	// when priority is not specified (0), then the order in which they
	// were added is used.
	priorities map[int]struct{}
}

// TopicRule represents a single routing rule.
type TopicRule struct {
	// Match is the CEL expression to match on the CloudEvent envelope.
	Match string `json:"match"`
	// Path is the HTTP path to post the event to (passed as Path in gRPC).
	Path string `json:"path"`
	// priority is the optional priority order (low to high) for this rule.
	priority int `json:"-"`
}

// NewTopicSubscription creates a new `TopicSubscription`.
func NewTopicSubscription(pubsubName, topic string) *TopicSubscription {
	return &TopicSubscription{ //nolint:exhaustivestruct
		PubsubName: pubsubName,
		Topic:      topic,
	}
}

// SetMetadata sets the metadata for the subscription if not already set.
// An error is returned if it is already set.
func (s *TopicSubscription) SetMetadata(metadata map[string]string) error {
	if s.Metadata != nil {
		return fmt.Errorf("subscription for topic %s on pubsub %s already has metadata set", s.Topic, s.PubsubName)
	}
	s.Metadata = metadata

	return nil
}

// SetDefaultRoute sets the default route if not already set.
// An error is returned if it is already set.
func (s *TopicSubscription) SetDefaultRoute(path string) error {
	if s.Routes == nil {
		if s.Route != "" {
			return fmt.Errorf("subscription for topic %s on pubsub %s already has route %s", s.Topic, s.PubsubName, s.Route)
		}
		s.Route = path
	} else {
		if s.Routes.Default != "" {
			return fmt.Errorf("subscription for topic %s on pubsub %s already has route %s", s.Topic, s.PubsubName, s.Routes.Default)
		}
		s.Routes.Default = path
	}

	return nil
}

// AddRoutingRule adds a routing rule.
// An error is returned if a there id a duplicate priority > 1.
func (s *TopicSubscription) AddRoutingRule(path, match string, priority int) error {
	if path == "" {
		return errors.New("path is required for routing rules")
	}
	if s.Routes == nil {
		s.Routes = &TopicRoutes{ //nolint:exhaustivestruct
			Default:    s.Route,
			priorities: map[int]struct{}{},
		}
		s.Route = ""
	}
	if priority > 0 {
		if _, exists := s.Routes.priorities[priority]; exists {
			return fmt.Errorf("subscription for topic %s on pubsub %s already has a routing rule with priority %d", s.Topic, s.PubsubName, priority)
		}
	}
	s.Routes.Rules = append(s.Routes.Rules, TopicRule{
		Match:    match,
		Path:     path,
		priority: priority,
	})
	sort.SliceStable(s.Routes.Rules, func(i, j int) bool {
		return s.Routes.Rules[i].priority < s.Routes.Rules[j].priority
	})
	if priority > 0 {
		s.Routes.priorities[priority] = struct{}{}
	}

	return nil
}
