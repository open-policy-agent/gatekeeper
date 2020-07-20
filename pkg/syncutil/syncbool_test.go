package syncutil_test

import (
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"
)

// Verifies changes are visible across goroutines.
func Test_SyncBool(t *testing.T) {
	var b syncutil.SyncBool
	done := make(chan struct{})

	go func() {
		defer close(done)

		for b.Get() == false {
		}
	}()

	go func() {
		b.Set(true)
	}()

	select {
	case <-time.After(10 * time.Millisecond):
		t.Errorf("failed waiting for flag visibility across goroutines")
	case <-done:
		// Success
	}
}
