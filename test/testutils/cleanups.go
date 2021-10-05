package testutils

import (
	"context"
	"os"
	"sync"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func UnsetEnv(t *testing.T, key string) func() {
	return func() {
		err := os.Unsetenv(key)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// DeleteObject deletes obj from c.
// Succeeds on success or if obj already did not exist in c.
// Fails the test if calling Delete() returns an error other than NotFound.
func DeleteObject(t *testing.T, c client.Client, obj client.Object) func() {
	// We don't want this cleanup method to rely on any context passed by the caller. For example, the caller may have
	// canceled their context as part of their test.
	ctx := context.Background()

	objCopy := obj.DeepCopyObject()
	obj, ok := objCopy.(client.Object)
	if !ok {
		t.Fatalf("got obj.DeepCopyObject() type %T, want %T",
			objCopy, client.Object(nil))
	}

	return func() {
		err := c.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("cleaning up %v %v/%v: %v",
				obj.GetObjectKind().GroupVersionKind(),
				obj.GetNamespace(), obj.GetName(), err)
		}
	}
}

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
