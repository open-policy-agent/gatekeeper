package testutils

import (
	"context"
	"sync"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// StartManager starts mgr. Registers a cleanup function to stop the manager at the completion of the test.
func StartManager(ctx context.Context, t *testing.T, mgr manager.Manager) {
	ctx, cancel := context.WithCancel(ctx)

	mgrStopped := &sync.WaitGroup{}
	mgrStopped.Add(1)

	var err error
	go func() {
		defer mgrStopped.Done()
		err = mgr.Start(ctx)
	}()

	t.Cleanup(func() {
		cancel()

		mgrStopped.Wait()
		if err != nil {
			t.Errorf("running Manager: %v", err)
		}
	})
}
