package instances

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestRouteConflictEventsRoutesByKind(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan event.GenericEvent, 4)
	assignCh := make(chan event.GenericEvent, 1)
	modifySetCh := make(chan event.GenericEvent, 1)
	assignImageCh := make(chan event.GenericEvent, 1)
	done := make(chan error, 1)

	go func() {
		done <- routeConflictEvents(ctx, events, assignCh, modifySetCh, assignImageCh)
	}()

	events <- makeGenericEvent("Assign", "assign")
	events <- makeGenericEvent("ModifySet", "modifyset")
	events <- makeGenericEvent("AssignImage", "assignimage")
	close(events)

	if got := receiveEvent(t, assignCh).Object.GetName(); got != "assign" {
		t.Fatalf("assign channel got %q, want %q", got, "assign")
	}
	if got := receiveEvent(t, modifySetCh).Object.GetName(); got != "modifyset" {
		t.Fatalf("modifySet channel got %q, want %q", got, "modifyset")
	}
	if got := receiveEvent(t, assignImageCh).Object.GetName(); got != "assignimage" {
		t.Fatalf("assignImage channel got %q, want %q", got, "assignimage")
	}

	ensureNoEvent(t, assignCh)
	ensureNoEvent(t, modifySetCh)
	ensureNoEvent(t, assignImageCh)
	awaitRouterExit(t, done)
}

func TestRouteConflictEventsBackpressure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan event.GenericEvent, 3)
	assignCh := make(chan event.GenericEvent, 1)
	modifySetCh := make(chan event.GenericEvent, 1)
	assignImageCh := make(chan event.GenericEvent, 1)
	done := make(chan error, 1)

	go func() {
		done <- routeConflictEvents(ctx, events, assignCh, modifySetCh, assignImageCh)
	}()

	events <- makeGenericEvent("Assign", "assign-1")
	events <- makeGenericEvent("Assign", "assign-2")
	events <- makeGenericEvent("ModifySet", "modifyset-1")

	awaitCondition(t, func() bool {
		return len(assignCh) == 1 && len(events) == 1
	}, "router did not block on the full assign queue")
	ensureNoEvent(t, modifySetCh)

	if got := receiveEvent(t, assignCh).Object.GetName(); got != "assign-1" {
		t.Fatalf("assign channel got %q, want %q", got, "assign-1")
	}
	if got := receiveEvent(t, assignCh).Object.GetName(); got != "assign-2" {
		t.Fatalf("assign channel got %q, want %q", got, "assign-2")
	}
	if got := receiveEvent(t, modifySetCh).Object.GetName(); got != "modifyset-1" {
		t.Fatalf("modifySet channel got %q, want %q", got, "modifyset-1")
	}

	close(events)
	awaitRouterExit(t, done)
}

func makeGenericEvent(kind, name string) event.GenericEvent {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "mutations.gatekeeper.sh", Kind: kind})
	obj.SetName(name)

	return event.GenericEvent{Object: obj}
}

func receiveEvent(t *testing.T, ch <-chan event.GenericEvent) event.GenericEvent {
	t.Helper()

	select {
	case evt := <-ch:
		return evt
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for routed event")
		return event.GenericEvent{}
	}
}

func ensureNoEvent(t *testing.T, ch <-chan event.GenericEvent) {
	t.Helper()

	select {
	case evt := <-ch:
		t.Fatalf("unexpected event for %s", evt.Object.GetName())
	case <-time.After(50 * time.Millisecond):
	}
}

func awaitRouterExit(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("router exited with error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for router to exit")
	}
}

func awaitCondition(t *testing.T, cond func() bool, message string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal(message)
}
