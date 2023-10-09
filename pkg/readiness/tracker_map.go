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

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type trackerMap struct {
	mu          sync.RWMutex
	m           map[schema.GroupVersionKind]*objectTracker
	removed     map[schema.GroupVersionKind]struct{}
	tryCanceled map[schema.GroupVersionKind]objData
	fn          objDataFactory
}

func newTrackerMap(fn objDataFactory) *trackerMap {
	if fn == nil {
		fn = objDataFromFlags
	}

	return &trackerMap{
		m:           make(map[schema.GroupVersionKind]*objectTracker),
		removed:     make(map[schema.GroupVersionKind]struct{}),
		tryCanceled: make(map[schema.GroupVersionKind]objData),
		fn:          fn,
	}
}

// Has returns true if the map is tracking the requested resource kind.
func (t *trackerMap) Has(gvk schema.GroupVersionKind) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, ok := t.m[gvk]
	return ok
}

// Get returns an objectTracker for the requested resource kind.
// A new one is created if the resource was not previously tracked.
func (t *trackerMap) Get(gvk schema.GroupVersionKind) Expectations {
	if entry := func() Expectations {
		t.mu.RLock()
		defer t.mu.RUnlock()

		if _, ok := t.removed[gvk]; ok {
			// Return a throwaway tracker if it was previously removed.
			return noopExpectations{}
		}
		if e, ok := t.m[gvk]; ok {
			return e
		}
		return nil // avoids https://golang.org/doc/faq#nil_error
	}(); entry != nil {
		return entry
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	// re-retrieve map entry in case it was added after releasing the read lock.
	if e, ok := t.m[gvk]; ok {
		return e
	}
	entry := newObjTracker(gvk, t.fn)
	t.m[gvk] = entry
	return entry
}

// Keys returns the resource kinds currently being tracked.
func (t *trackerMap) Keys() []schema.GroupVersionKind {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]schema.GroupVersionKind, 0, len(t.m))
	for k := range t.m {
		out = append(out, k)
	}
	return out
}

// Remove stops tracking a resource kind. It cannot be tracked again by the same map.
func (t *trackerMap) Remove(gvk schema.GroupVersionKind) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.removeNoLock(gvk)
}

func (t *trackerMap) removeNoLock(gvk schema.GroupVersionKind) {
	delete(t.m, gvk)
	delete(t.tryCanceled, gvk)

	t.removed[gvk] = struct{}{}
}

// Satisfied returns true if all tracked expectations have been satisfied.
func (t *trackerMap) Satisfied() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, ot := range t.m {
		if !ot.Satisfied() {
			return false
		}
	}
	return true
}

// Populated returns true if all objectTrackers are populated.
func (t *trackerMap) Populated() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, ot := range t.m {
		if !ot.Populated() {
			return false
		}
	}
	return true
}

// Populated returns true if all objectTrackers are populated.
func (t *trackerMap) TryCancel(g schema.GroupVersionKind) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	obj, ok := t.tryCanceled[g]
	if !ok {
		// need to create a record of this TryCancel call
		obj = t.fn()
	}

	shouldDel := obj.decrementRetries()
	t.tryCanceled[g] = obj // set the changed obj back to the map, as the value is not a pointer

	if shouldDel {
		t.removeNoLock(g)
	}

	return shouldDel
}
