package instances

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// gatekeeperInstancesAdderSubprocessEnvVar marks a re-exec'd child process that
// actually calls Adder.Add. pkg/operations only allows the process-wide
// "operation" flag to be narrowed once (subsequent Set calls add to the
// existing set rather than replacing it), so TestAddSkipsRegistrationWhenMutationDisabled
// and TestAddProceedsWhenMutationEnabled each run their assertion in a fresh
// subprocess instead of sharing that global state.
const gatekeeperInstancesAdderSubprocessEnvVar = "GATEKEEPER_INSTANCES_ADDER_SUBPROCESS"

// gatekeeperTestOperationEnvVar carries the desired --operation value into the subprocess.
const gatekeeperTestOperationEnvVar = "GATEKEEPER_TEST_OPERATION"

// runAddInSubprocess re-execs the current test binary, running only testName with
// the "operation" flag set to op before Add is called.
func runAddInSubprocess(t *testing.T, testName, op string) (string, error) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(),
		gatekeeperInstancesAdderSubprocessEnvVar+"=1",
		gatekeeperTestOperationEnvVar+"="+op,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func setOperationFromEnv(t *testing.T) {
	t.Helper()

	op := os.Getenv(gatekeeperTestOperationEnvVar)
	if err := flag.CommandLine.Set("operation", op); err != nil {
		t.Fatalf("setting operation flag to %q: %v", op, err)
	}
}

// TestAddSkipsRegistrationWhenMutationDisabled guards against unconditionally
// registering the conflict-routing runnable (and the mutator controllers behind
// it) for pods that have no mutation operation assigned. Before this guard,
// Add dereferenced the manager (e.g. mgr.GetScheme()) before ever consulting
// mutation.Enabled(), so passing a nil manager panics unless the early return
// below is in place.
func TestAddSkipsRegistrationWhenMutationDisabled(t *testing.T) {
	if os.Getenv(gatekeeperInstancesAdderSubprocessEnvVar) == "1" {
		setOperationFromEnv(t)

		a := &Adder{}
		if err := a.Add(nil); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
		return
	}

	out, err := runAddInSubprocess(t, "TestAddSkipsRegistrationWhenMutationDisabled", string(operations.Audit))
	if err != nil {
		t.Fatalf("audit-only Add(nil) should return nil without touching the manager: %v\n%s", err, out)
	}
}

// TestAddProceedsWhenMutationEnabled is the mutation-only counterpart to
// TestAddSkipsRegistrationWhenMutationDisabled: it verifies Add does not take the
// early-return path when a mutation operation is assigned. It can't exercise full
// controller registration without a real manager, so it instead confirms Add moves
// past the mutation.Enabled() guard by asserting it panics on mgr.GetScheme()
// against the nil manager, the same signal the pre-fix code panicked on unconditionally.
func TestAddProceedsWhenMutationEnabled(t *testing.T) {
	if os.Getenv(gatekeeperInstancesAdderSubprocessEnvVar) == "1" {
		setOperationFromEnv(t)

		defer func() {
			if r := recover(); r == nil {
				t.Fatal("Add(nil) returned without touching the manager; expected it to proceed past the mutation.Enabled() guard and panic dereferencing the nil manager")
			}
		}()

		a := &Adder{}
		_ = a.Add(nil)
		return
	}

	out, err := runAddInSubprocess(t, "TestAddProceedsWhenMutationEnabled", string(operations.MutationWebhook))
	if err != nil {
		t.Fatalf("mutation-webhook-only Add(nil) subprocess failed: %v\n%s", err, out)
	}
}

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
