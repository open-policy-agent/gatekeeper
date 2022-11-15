package webhook

import (
	"fmt"
	"reflect"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestCongifureWebhookServer(t *testing.T) {
	expectedServer := &webhook.Server{
		TLSMinVersion: "1.3",
	}

	if *clientCAName != "" {
		expectedServer.ClientCAName = *clientCAName
	}

	tc := []struct {
		Name           string
		Server         *webhook.Server
		ExpectedServer *webhook.Server
	}{
		{
			Name:           "Wbhook server config",
			Server:         &webhook.Server{},
			ExpectedServer: expectedServer,
		},
	}

	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			server := congifureWebhookServer(tt.Server)
			expectedServer.TLSOpts = server.TLSOpts

			if !reflect.DeepEqual(tt.ExpectedServer, server) {
				t.Errorf(fmt.Sprintf("got %#v, want %#v", server, tt.ExpectedServer))
			}
		})
	}
}
