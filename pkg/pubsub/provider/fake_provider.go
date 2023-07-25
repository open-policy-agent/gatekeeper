package provider

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/dapr"
)

func FakeProviders() {
	pubSubs = newPubSubSet(map[string]InitiateConnection{
		dapr.Name: dapr.FakeNewConnection,
	})
}
