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
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// ObjectTracker tracks expectations for runtime.Objects.
// A set of Expect() calls are made, demarcated by ExpectationsDone().
// Expectations are satisfied by calls to Observe().
// Once all expectations are satisfied, Satisfied() will begin returning true.
type ObjectTracker struct {
	mu        sync.Mutex
	gvk       schema.GroupVersionKind
	cancelled objSet
	expect    objSet
	seen      objSet
	satisfied objSet
	populated bool
}

func newObjTracker(gvk schema.GroupVersionKind) *ObjectTracker {
	return &ObjectTracker{
		gvk:       gvk,
		cancelled: make(objSet),
		expect:    make(objSet),
		seen:      make(objSet),
		satisfied: make(objSet),
	}
}

// Expect sets an expectation that must be met by a corresponding call to Observe().
func (t *ObjectTracker) Expect(o runtime.Object) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Only accept expectations until we're marked as fully populated.
	if t.populated {
		return
	}

	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return
	}

	// Cancelled objects cannot be expected again.
	if _, ok := t.cancelled[k]; ok {
		return
	}

	// We may have seen it before starting to expect it
	if _, ok := t.seen[k]; ok {
		delete(t.seen, k)
		t.satisfied[k] = struct{}{}
	}

	t.expect[k] = struct{}{}
}

// CancelExpect cancels an expectation and marks it so it
// cannot be expected again going forward.
func (t *ObjectTracker) CancelExpect(o runtime.Object) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return
	}

	delete(t.expect, k)
	delete(t.seen, k)
	delete(t.satisfied, k)
	t.cancelled[k] = struct{}{}
}

// ExpectationsDone tells the tracker to stop accepting new expectations.
// Only expectations set before ExpectationsDone is called will be considered
// in Satisfied().
func (t *ObjectTracker) ExpectationsDone() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.populated = true
}

// Unsatisfied returns all unsatisfied expectations
func (t *ObjectTracker) Unsatisfied() []runtime.Object {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]runtime.Object, 0, len(t.expect))
	for k := range t.expect {
		if _, ok := t.satisfied[k]; ok {
			continue
		}
		out = append(out, k)
	}
	return out
}

// Observe makes an observation. Observations can be made before expectations and vice-versa.
func (t *ObjectTracker) Observe(o runtime.Object) {
	t.mu.Lock()
	defer t.mu.Unlock()

	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return
	}

	// Ignore cancelled expectations
	if _, ok := t.cancelled[k]; ok {
		return
	}

	if _, ok := t.expect[k]; ok {
		// Satisfy existing expectation
		delete(t.seen, k)
		t.satisfied[k] = struct{}{}
		return
	}

	t.seen[k] = struct{}{}
}

// Satisfied returns true if all expectations have been satisfied.
// Also returns false if ExpectationsDone() has not been called.
func (t *ObjectTracker) Satisfied() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.populated {
		return false
	}

	if len(t.satisfied) == len(t.expect) {
		return true
	}

	// Resolve any expectations where the observation preceded the expect request.
	for k := range t.seen {
		if _, ok := t.expect[k]; !ok {
			continue
		}
		delete(t.seen, k)
		t.satisfied[k] = struct{}{}
	}

	return len(t.satisfied) == len(t.expect)
}

// objKeyFromObject constructs an objKey representing the provided runtime.Object.
func objKeyFromObject(obj runtime.Object) (objKey, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return objKey{}, err
	}

	// TODO - Cheat because sometimes APIVersion / Kind are empty
	var gvk schema.GroupVersionKind
	switch {
	case isCT(obj):
		gvk = constraintTemplateGVK
	default:
		gvk = obj.GetObjectKind().GroupVersionKind()
	}

	nn := types.NamespacedName{Namespace: accessor.GetNamespace(), Name: accessor.GetName()}
	return objKey{namespacedName: nn, gvk: gvk}, nil
}
