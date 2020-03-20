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

package watch

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Set tracks a set of watched resource GVKs.
type Set struct {
	mux sync.RWMutex
	set map[schema.GroupVersionKind]bool
}

// NewSet constructs a new watchSet.
func NewSet() *Set {
	return &Set{
		set: make(map[schema.GroupVersionKind]bool),
	}
}

func (w *Set) Size() int {
	w.mux.RLock()
	defer w.mux.RUnlock()
	return len(w.set)
}

func (w *Set) Items() []schema.GroupVersionKind {
	w.mux.RLock()
	defer w.mux.RUnlock()
	var r []schema.GroupVersionKind
	for k := range w.set {
		r = append(r, k)
	}
	return r
}

func (w *Set) String() string {
	gvks := w.Items()
	var strs []string
	for _, gvk := range gvks {
		strs = append(strs, gvk.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(strs, ", "))
}

func (w *Set) Add(gvks ...schema.GroupVersionKind) {
	w.mux.Lock()
	defer w.mux.Unlock()
	for _, gvk := range gvks {
		w.set[gvk] = true
	}
}

func (w *Set) Remove(gvks ...schema.GroupVersionKind) {
	w.mux.Lock()
	defer w.mux.Unlock()
	for _, gvk := range gvks {
		delete(w.set, gvk)
	}
}

func (w *Set) Dump() map[schema.GroupVersionKind]bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	m := make(map[schema.GroupVersionKind]bool, len(w.set))
	for k, v := range w.set {
		m[k] = v
	}
	return m
}

func (w *Set) AddSet(other *Set) {
	s := other.Dump()
	w.mux.Lock()
	defer w.mux.Unlock()
	for k := range s {
		w.set[k] = true
	}
}

func (w *Set) RemoveSet(other *Set) {
	s := other.Dump()
	w.mux.Lock()
	defer w.mux.Unlock()
	for k := range s {
		delete(w.set, k)
	}
}

func (w *Set) Equals(other *Set) bool {
	otherSet := other.Dump()
	w.mux.RLock()
	defer w.mux.RUnlock()
	return reflect.DeepEqual(w.set, otherSet)
}

func (w *Set) Replace(other *Set) {
	otherSet := other.Dump()
	w.mux.Lock()
	defer w.mux.Unlock()

	newSet := make(map[schema.GroupVersionKind]bool)
	for k, v := range otherSet {
		newSet[k] = v
	}
	w.set = newSet
}

func (w *Set) Contains(gvk schema.GroupVersionKind) bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	return w.set[gvk]
}

// Difference returns items in the set that are not in the other (provided) set.
func (w *Set) Difference(other *Set) *Set {
	s := other.Dump()
	w.mux.RLock()
	defer w.mux.RUnlock()

	out := make(map[schema.GroupVersionKind]bool)
	for k := range w.set {
		if s[k] {
			continue
		}
		out[k] = true
	}
	return &Set{set: out}
}

// Union returns a set composed of all items that are both in set w and other.
func (w *Set) Union(other *Set) *Set {
	s := other.Dump()
	w.mux.RLock()
	defer w.mux.RUnlock()

	out := make(map[schema.GroupVersionKind]bool)
	for k := range w.set {
		if !s[k] {
			continue
		}
		out[k] = true
	}
	return &Set{set: out}
}
