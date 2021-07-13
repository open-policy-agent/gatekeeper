/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package readiness

import (
	"flag"
	"fmt"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var readinessRetries = flag.Int("readiness-retries", 0, "The number of resource ingestion attempts allowed before the resource is disregarded.  A value of -1 will retry indefinitely.")

// Expectations tracks expectations for runtime.Objects.
// A set of Expect() calls are made, demarcated by ExpectationsDone().
// Expectations are satisfied by calls to Observe().
// Once all expectations are satisfied, Satisfied() will begin returning true.
type Expectations interface {
	Expect(o runtime.Object)
	CancelExpect(o runtime.Object)
	TryCancelExpect(o runtime.Object) bool
	ExpectationsDone()
	Observe(o runtime.Object)
	Satisfied() bool
	Populated() bool
}

// objectTracker tracks expectations for runtime.Objects.
// A set of Expect() calls are made, demarcated by ExpectationsDone().
// Expectations are satisfied by calls to Observe().
// Once all expectations are satisfied, Satisfied() will begin returning true.
type objectTracker struct {
	mu            sync.RWMutex
	gvk           schema.GroupVersionKind
	canceled      objSet                    // expectations that have been canceled
	expect        objSet                    // unresolved expectations
	tryCanceled   objRetrySet               // tracks TryCancelExpect calls, decrementing allotted retries for an object
	seen          objSet                    // observations made before their expectations
	satisfied     objSet                    // tracked to avoid re-adding satisfied expectations and to support unsatisfied()
	populated     bool                      // all expectations have been provided
	allSatisfied  bool                      // true once all expectations have been satisfied. Acts as a circuit-breaker.
	kindsSnapshot []schema.GroupVersionKind // Snapshot of kinds before freeing memory in Satisfied.
	tryCancelObj  objDataFactory            // Function that creates objData types used in tryCanceled
}

func newObjTracker(gvk schema.GroupVersionKind, fn objDataFactory) *objectTracker {
	if fn == nil {
		fn = objDataFromFlags
	}

	return &objectTracker{
		gvk:          gvk,
		canceled:     make(objSet),
		expect:       make(objSet),
		tryCanceled:  make(objRetrySet),
		seen:         make(objSet),
		satisfied:    make(objSet),
		tryCancelObj: fn,
	}
}

// Expect sets an expectation that must be met by a corresponding call to Observe().
func (t *objectTracker) Expect(o runtime.Object) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Only accept expectations until we're marked as fully populated.
	if t.populated {
		return
	}

	// Don't expect resources which are being terminated.
	accessor, err := meta.Accessor(o)
	if err == nil && !accessor.GetDeletionTimestamp().IsZero() {
		return
	}

	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return
	}

	// Canceled objects cannot be expected again.
	if _, ok := t.canceled[k]; ok {
		return
	}

	// We may have seen it before starting to expect it
	if _, ok := t.seen[k]; ok {
		delete(t.seen, k)
		delete(t.expect, k)
		t.satisfied[k] = struct{}{}
		return
	}

	t.expect[k] = struct{}{}
}

// nolint: gocritic // Using a pointer here is less efficient and results in more copying.
func (t *objectTracker) cancelExpectNoLock(k objKey) {
	delete(t.expect, k)
	delete(t.seen, k)
	delete(t.satisfied, k)
	delete(t.tryCanceled, k)
	t.canceled[k] = struct{}{}
}

// CancelExpect cancels an expectation and marks it so it
// cannot be expected again going forward.
func (t *objectTracker) CancelExpect(o runtime.Object) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Respect circuit-breaker.
	if t.allSatisfied {
		return
	}

	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return
	}

	t.cancelExpectNoLock(k)
}

// TryCancelExpect will check the readinessRetries left on an Object, and cancel
// the expectation for that object if no retries remain.  Returns True if the
// expectation was canceled.
func (t *objectTracker) TryCancelExpect(o runtime.Object) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Respect circuit-breaker.
	if t.allSatisfied {
		return false
	}

	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return false
	}

	// Check if it's time to delete an expectation or just decrement its allotted retries
	obj, ok := t.tryCanceled[k]
	if !ok {
		// If the item isn't in the map, add it.  This is the only place t.newObjData() should be called.
		obj = t.tryCancelObj()
	}
	shouldDel := obj.decrementRetries()
	t.tryCanceled[k] = obj // set the changed obj back to the map, as the value is not a pointer

	if shouldDel {
		t.cancelExpectNoLock(k)
	}

	return shouldDel
}

// ExpectationsDone tells the tracker to stop accepting new expectations.
// Only expectations set before ExpectationsDone is called will be considered
// in Satisfied().
func (t *objectTracker) ExpectationsDone() {
	t.mu.Lock()
	defer t.mu.Unlock()

	log.Info("ExpectationsDone", "gvk", t.gvk, "expectationCount", len(t.expect)+len(t.satisfied))
	t.populated = true
}

// Unsatisfied returns all unsatisfied expectations.
func (t *objectTracker) unsatisfied() []objKey {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]objKey, 0, len(t.expect))
	for k := range t.expect {
		if _, ok := t.satisfied[k]; ok {
			continue
		}
		out = append(out, k)
	}
	return out
}

// Observe makes an observation. Observations can be made before expectations and vice-versa.
func (t *objectTracker) Observe(o runtime.Object) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Respect circuit-breaker.
	if t.allSatisfied {
		return
	}

	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return
	}

	// Ignore canceled expectations
	if _, ok := t.canceled[k]; ok {
		return
	}

	// Ignore satisfied expectations
	if _, ok := t.satisfied[k]; ok {
		return
	}

	_, wasExpecting := t.expect[k]
	switch {
	case wasExpecting:
		// Satisfy existing expectation
		delete(t.seen, k)
		delete(t.expect, k)
		t.satisfied[k] = struct{}{}
		return
	case !wasExpecting && t.populated:
		// Not expecting and no longer accepting expectations.
		// No need to track.
		delete(t.seen, k)
		return
	}

	// Track for future expectation.
	t.seen[k] = struct{}{}
}

func (t *objectTracker) Populated() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.populated
}

// Satisfied returns true if all expectations have been satisfied.
// Expectations must be populated before the tracker can be considered satisfied.
// Expectations are marked as populated by calling ExpectationsDone().
func (t *objectTracker) Satisfied() bool {
	// Determine if we need to acquire a write lock, which blocks concurrent access
	satisfied, needMutate := func() (bool, bool) {
		t.mu.RLock()
		defer t.mu.RUnlock()

		// matching observations and expectations may be able to be resolved
		resolvableExpectations := len(t.seen) > 0 && len(t.expect) > 0

		// We only need the write lock when all of the following are true:
		//  1. We haven't yet tripped the circuit breaker (t.allSatisfied)
		//  2. We have received all necessary expectations (t.populated)
		//  3. There is potential for action to be taken
		//     a. There are resolvableExpectations
		//     - OR -
		//     b. There are no expectations.  I.e. we are ready to declare t.allSatisfied = true
		needMutate := !t.allSatisfied && t.populated &&
			(resolvableExpectations || len(t.expect) == 0)

		return t.allSatisfied, needMutate
	}()

	if satisfied {
		return true
	}

	// Proceed only if we have state changes to make.
	if !needMutate {
		log.V(1).Info("readiness state", "gvk", t.gvk, "satisfied", fmt.Sprintf("%d/%d", len(t.satisfied), len(t.expect)+len(t.satisfied)))
		return false
	}

	// From here we need a write lock to mutate state.
	t.mu.Lock()
	defer t.mu.Unlock()

	// Resolve any expectations where the observation preceded the expect request.
	var resolveCount int
	for k := range t.seen {
		if _, ok := t.expect[k]; !ok {
			continue
		}
		delete(t.seen, k)
		delete(t.expect, k)
		t.satisfied[k] = struct{}{}
		resolveCount++
	}
	log.V(1).Info("resolved pre-observations", "gvk", t.gvk, "count", resolveCount)
	log.V(1).Info("readiness state", "gvk", t.gvk, "satisfied", fmt.Sprintf("%d/%d", len(t.satisfied), len(t.expect)+len(t.satisfied)))

	// All satisfied if:
	//  1. Expectations have been previously populated
	//  2. No expectations remain
	if t.populated && len(t.expect) == 0 {
		t.allSatisfied = true
		log.V(1).Info("all expectations satisfied", "gvk", t.gvk)

		// Circuit-breaker tripped - free tracking memory
		t.kindsSnapshot = t.kindsNoLock() // Take snapshot as kinds() depends on the maps we're about to clear.
		t.seen = nil
		t.expect = nil
		t.satisfied = nil
		t.canceled = nil
		t.tryCanceled = nil
	}
	return t.allSatisfied
}

func (t *objectTracker) kinds() []schema.GroupVersionKind {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.kindsNoLock()
}

func (t *objectTracker) kindsNoLock() []schema.GroupVersionKind {
	if t.kindsSnapshot != nil {
		out := make([]schema.GroupVersionKind, len(t.kindsSnapshot))
		copy(out, t.kindsSnapshot)
		return out
	}

	m := make(map[schema.GroupVersionKind]struct{})
	for k := range t.satisfied {
		m[k.gvk] = struct{}{}
	}
	for k := range t.expect {
		m[k.gvk] = struct{}{}
	}

	if len(m) == 0 {
		return nil
	}

	out := make([]schema.GroupVersionKind, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// objKeyFromObject constructs an objKey representing the provided runtime.Object.
func objKeyFromObject(obj runtime.Object) (objKey, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return objKey{}, err
	}

	// Index ConstraintTemplates by their corresponding constraint GVK.
	// This will be leveraged in tracker.Satisfied().
	var gvk schema.GroupVersionKind
	switch v := obj.(type) {
	case *templates.ConstraintTemplate:
		gvk = schema.GroupVersionKind{
			Group:   constraintGroup,
			Version: v1beta1.SchemeGroupVersion.Version,
			Kind:    v.Spec.CRD.Spec.Names.Kind,
		}
	case *v1beta1.ConstraintTemplate:
		gvk = schema.GroupVersionKind{
			Group:   constraintGroup,
			Version: v1beta1.SchemeGroupVersion.Version,
			Kind:    v.Spec.CRD.Spec.Names.Kind,
		}
	case *unstructured.Unstructured:
		ugvk := obj.GetObjectKind().GroupVersionKind()
		if ugvk.GroupVersion() == v1beta1.SchemeGroupVersion && ugvk.Kind == "ConstraintTemplate" {
			cKind, found, err := unstructured.NestedString(v.Object, "spec", "crd", "spec", "names", "kind")
			if !found || err != nil {
				return objKey{}, errors.Wrapf(err, "retrieving nested CRD Kind field for unstructured. Found: %v Object: %v", found, obj)
			}
			gvk = schema.GroupVersionKind{
				Group:   constraintGroup,
				Version: v1beta1.SchemeGroupVersion.Version,
				Kind:    cKind,
			}
		} else {
			gvk = ugvk
		}
	default:
		gvk = obj.GetObjectKind().GroupVersionKind()
	}

	nn := types.NamespacedName{Namespace: accessor.GetNamespace(), Name: accessor.GetName()}
	return objKey{namespacedName: nn, gvk: gvk}, nil
}

// IsExpecting returns true if the gvk/name combination was previously expected by the tracker.
// Only valid until allSatisfied==true as tracking memory is freed at that point.
// For testing only.
func (t *objectTracker) IsExpecting(gvk schema.GroupVersionKind, nsName types.NamespacedName) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	k := objKey{gvk: gvk, namespacedName: nsName}
	if _, ok := t.expect[k]; ok {
		return true
	}
	if _, ok := t.satisfied[k]; ok {
		return true
	}
	return false
}
