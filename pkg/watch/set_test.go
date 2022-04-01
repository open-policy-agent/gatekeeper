package watch

import (
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func write(ints chan schema.GroupVersionKind, toWrite schema.GroupVersionKind) func() {
	return func() {
		ints <- toWrite
	}
}

func TestSet_Replace_Race(t *testing.T) {
	// CAUTION: This is a race condition test. It should not be flaky.
	// If you see failures in CI, run this test with --count=1000 to induce failures.

	// Create three sets. The first set is just a starter. We'll be replacing it
	// with the two latter sets at the same time.
	namespaceGVK := corev1.SchemeGroupVersion.WithKind("Namespace")
	roleGVK := rbacv1.SchemeGroupVersion.WithKind("Role")
	clusterRoleGVK := rbacv1.SchemeGroupVersion.WithKind("ClusterRole")

	s := NewSet()
	s.Add(namespaceGVK)

	other1 := NewSet()
	other1.Add(roleGVK)
	other2 := NewSet()
	other2.Add(clusterRoleGVK)

	// Create two channels which are written to after each Replace succeeds, but
	// before releasing locks. We expect identical data to be written to both
	// channels. This simulates other work being done.
	replaceA := make(chan schema.GroupVersionKind)
	replaceB := make(chan schema.GroupVersionKind)

	wait := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(4)

	go func() {
		<-wait
		s.Replace(other1, write(replaceA, roleGVK), write(replaceB, roleGVK))
		wg.Done()
	}()

	go func() {
		<-wait
		s.Replace(other2, write(replaceA, clusterRoleGVK), write(replaceB, clusterRoleGVK))
		wg.Done()
	}()

	// Start all goroutines simultaneously.
	// It doesn't matter which Replace finishes first or second - we actually
	// want it to be possible for either to win in order to ensure that deadlocks
	// don't happen and data is written consistently.
	close(wait)

	var a1, a2, b1, b2 schema.GroupVersionKind
	wg2 := sync.WaitGroup{}
	wg2.Add(2)

	// Read from the channels asynchronously to prevent blocking and inducing
	// successes.
	go func() {
		a1 = <-replaceA
		a2 = <-replaceA
		wg.Done()
	}()
	go func() {
		b1 = <-replaceB
		b2 = <-replaceB
		wg.Done()
	}()

	// Wait for the Replaces and reading the output data to complete before continuing on.
	wg.Wait()

	if a1 != b1 {
		t.Fatal("different writes to first element")
	}
	if a2 != b2 {
		t.Fatal("different writes to second element")
	}

	// Ensure that the last Replace call is the one which won. The goroutine which
	// wrote to b2 tells us this.
	want := b2
	got := s.Contains(want)
	if !got {
		t.Fatalf("%v Replace finished last but GVK not found in set", want)
	}

	// Check that the set does not contain the first-replaced GVK.
	notWant := b1
	got = s.Contains(notWant)
	if got {
		t.Fatalf("%v Replace finished first but GVK found in set", notWant)
	}
}
