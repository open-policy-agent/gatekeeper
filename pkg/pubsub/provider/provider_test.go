package provider

import (
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/dapr"
)

func Test_newPubSubSet(t *testing.T) {
	tests := []struct {
		name    string
		pubSubs map[string]InitiateConnection
		wantKey string
	}{
		{
			name: "only one provider is available",
			pubSubs: map[string]InitiateConnection{
				dapr.Name: dapr.NewConnection,
			},
			wantKey: dapr.Name,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newPubSubSet(tt.pubSubs)
			if _, ok := got.supportedPubSub[tt.wantKey]; !ok {
				t.Errorf("newPubSubSet() = %#v, want key %#v", got.supportedPubSub, tt.wantKey)
			}
		})
	}
}

func TestList(t *testing.T) {
	tests := []struct {
		name    string
		wantKey string
	}{
		{
			name:    "only one provider is available",
			wantKey: dapr.Name,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := List()
			if _, ok := got[tt.wantKey]; !ok {
				t.Errorf("List() = %#v, want key %#v", got, tt.wantKey)
			}
		})
	}
}
