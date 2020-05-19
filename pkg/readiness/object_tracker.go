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

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// Expectations tracks expectations for runtime.Objects.
// A set of Expect() calls are made, demarcated by ExpectationsDone().
// Expectations are satisfied by calls to Observe().
// Once all expectations are satisfied, Satisfied() will begin returning true.
type Expectations interface {
	Expect(o runtime.Object)
	CancelExpect(o runtime.Object)
	ExpectationsDone()
	Observe(o runtime.Object)
	Satisfied() bool
}

// objectTracker tracks expectations for runtime.Objects.
// A set of Expect() calls are made, demarcated by ExpectationsDone().
// Expectations are satisfied by calls to Observe().
// Once all expectations are satisfied, Satisfied() will begin returning true.
type objectTracker struct {
	mu        sync.RWMutex
	gvk       schema.GroupVersionKind
	cancelled objSet // expectations that have been cancelled
	expect    objSet // unresolved expectations
	seen      objSet // observations made before their expectations
	satisfied objSet // tracked to avoid re-adding satisfied expectations and to support unsatisfied()
	populated bool   // all expectations have been provided
}

func newObjTracker(gvk schema.GroupVersionKind) *objectTracker {
	return &objectTracker{
		gvk:       gvk,
		cancelled: make(objSet),
		expect:    make(objSet),
		seen:      make(objSet),
		satisfied: make(objSet),
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
		delete(t.expect, k)
		t.satisfied[k] = struct{}{}
		return
	}

	t.expect[k] = struct{}{}
}

// CancelExpect cancels an expectation and marks it so it
// cannot be expected again going forward.
func (t *objectTracker) CancelExpect(o runtime.Object) {
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
func (t *objectTracker) ExpectationsDone() {
	t.mu.Lock()
	defer t.mu.Unlock()

	log.Info("ExpectationsDone", "gvk", t.gvk, "expectationCount", len(t.expect)+len(t.satisfied))
	t.populated = true
}

// Unsatisfied returns all unsatisfied expectations
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

	k, err := objKeyFromObject(o)
	if err != nil {
		log.Error(err, "skipping")
		return
	}

	// Ignore cancelled expectations
	if _, ok := t.cancelled[k]; ok {
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
	satisfied, seenKeys := func() (bool, []objKey) {
		t.mu.RLock()
		defer t.mu.RUnlock()

		if !t.populated {
			return false, nil
		}

		if len(t.expect) == 0 {
			return true, nil
		}

		// Resolve any expectations where the observation preceded the expect request.
		var keys []objKey
		for k := range t.seen {
			if _, ok := t.expect[k]; !ok {
				continue
			}
			keys = append(keys, k)
		}
		return false, keys
	}()

	if len(seenKeys) == 0 {
		return satisfied
	}

	// From here we need a write lock to mutate state.
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, k := range seenKeys {
		delete(t.seen, k)
		delete(t.expect, k)
		t.satisfied[k] = struct{}{}
	}

	return len(t.expect) == 0
}

func (t *objectTracker) kinds() []schema.GroupVersionKind {
	t.mu.RLock()
	defer t.mu.RUnlock()

	l := len(t.satisfied) + len(t.expect)
	if l == 0 {
		return nil
	}

	out := make([]schema.GroupVersionKind, 0, l)
	for k := range t.satisfied {
		out = append(out, k.gvk)
	}
	for k := range t.expect {
		out = append(out, k.gvk)
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
	default:
		gvk = obj.GetObjectKind().GroupVersionKind()
	}

	nn := types.NamespacedName{Namespace: accessor.GetNamespace(), Name: accessor.GetName()}
	return objKey{namespacedName: nn, gvk: gvk}, nil
}
