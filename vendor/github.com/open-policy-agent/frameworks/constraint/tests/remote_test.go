package tests

import (
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/remote"
)

func TestRemoteClientE2E(t *testing.T) {
	d, err := remote.New(remote.URL("http://localhost:8181"), remote.Tracing(false))
	if err != nil {
		t.Fatal(err)
	}
	p, err := client.NewProbe(d)
	if err != nil {
		t.Fatal(err)
	}
	for name, f := range p.TestFuncs() {
		t.Run(name, func(t *testing.T) {
			if err := f(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
