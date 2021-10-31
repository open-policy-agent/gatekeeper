package testutils

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeleteObject deletes obj from c.
// Succeeds on success or if obj already did not exist in c.
// Fails the test if calling Delete() returns an error other than NotFound.
//
// Does not ensure the object is actually deleted, just sends a delete request via the passed Client.
// See #1596 for why this is non-trivial to guarantee.
//
// Tests should not rely on objects being deleted from the API Server which were
// used in other tests; they should instead use unique object names and
// namespaces to eliminate cross-talk. More explicitly: if you are writing a
// test which must make use of this cleanup function, you are risking cross-talk
// between tests and should instead modify your test.
func DeleteObject(t *testing.T, c client.Client, original client.Object) func() {
	// We don't want this cleanup method to rely on any context passed by the caller. For example, the caller may have
	// canceled their context as part of their test.
	ctx := context.Background()

	objCopy := original.DeepCopyObject()
	obj, ok := objCopy.(client.Object)
	if !ok {
		t.Fatalf("got DeepCopyObject(%T) = %T, which is not a client.Object", original, objCopy)
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
