package provider

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/dapr"
)

var pubSubs = newPubSubSet(map[string]InitiateConnection{
	dapr.Name: dapr.NewConnection,
},
)

type pubSubSet struct {
	supportedPubSub map[string]InitiateConnection
}

// returns new client for pub sub tool.
type InitiateConnection func(ctx context.Context, config interface{}) (connection.Connection, error)

func newPubSubSet(pubSubs map[string]InitiateConnection) *pubSubSet {
	supported := make(map[string]InitiateConnection)
	set := &pubSubSet{
		supportedPubSub: supported,
	}
	for name := range pubSubs {
		set.supportedPubSub[name] = pubSubs[name]
	}
	return set
}

func List() map[string]InitiateConnection {
	ret := make(map[string]InitiateConnection)
	for name, new := range pubSubs.supportedPubSub {
		ret[name] = new
	}
	return ret
}
