package provider

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/dapr"
)

var fakeProviderName = map[string]InitiateConnection{
	dapr.Name: dapr.FakeNewConnection,
}

func ListFakeProviders() map[string]InitiateConnection {
	ret := make(map[string]InitiateConnection)
	for name, new := range fakeProviderName {
		ret[name] = new
	}
	return ret
}
