package internal

import (
	"errors"
	"fmt"

	"github.com/dapr/go-sdk/service/common"
)

// TopicRegistrar is a map of <pubsubname>-<topic> to `TopicRegistration`
// and acts as a lookup as the application is building up subscriptions with
// potentially multiple routes per topic.
type TopicRegistrar map[string]*TopicRegistration

// TopicRegistration encapsulates the subscription and handlers.
type TopicRegistration struct {
	Subscription   *TopicSubscription
	DefaultHandler common.TopicEventHandler
	RouteHandlers  map[string]common.TopicEventHandler
}

func (m TopicRegistrar) AddSubscription(sub *common.Subscription, fn common.TopicEventHandler) error {
	if sub.Topic == "" {
		return errors.New("topic name required")
	}
	if sub.PubsubName == "" {
		return errors.New("pub/sub name required")
	}
	if fn == nil {
		return fmt.Errorf("topic handler required")
	}

	var key string
	if !sub.DisableTopicValidation {
		key = sub.PubsubName + "-" + sub.Topic
	} else {
		key = sub.PubsubName
	}

	ts, ok := m[key]
	if !ok {
		ts = &TopicRegistration{
			Subscription:   NewTopicSubscription(sub.PubsubName, sub.Topic),
			RouteHandlers:  make(map[string]common.TopicEventHandler),
			DefaultHandler: nil,
		}
		ts.Subscription.SetMetadata(sub.Metadata)
		m[key] = ts
	}

	if sub.Match != "" {
		if err := ts.Subscription.AddRoutingRule(sub.Route, sub.Match, sub.Priority); err != nil {
			return err
		}
	} else {
		if err := ts.Subscription.SetDefaultRoute(sub.Route); err != nil {
			return err
		}
		ts.DefaultHandler = fn
	}
	ts.RouteHandlers[sub.Route] = fn

	return nil
}
