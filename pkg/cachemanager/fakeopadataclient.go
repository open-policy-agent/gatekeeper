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
package cachemanager

import (
	"context"
	"fmt"
	gosync "sync"

	constraintTypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type OpaKey struct {
	Gvk schema.GroupVersionKind
	Key string
}

// FakeOpa is an OpaDataClient for testing.
type FakeOpa struct {
	mu           gosync.Mutex
	data         map[OpaKey]interface{}
	needsToError bool
}

var _ OpaDataClient = &FakeOpa{}

// keyFor returns an opaKey for the provided resource.
// Returns error if the resource is not a runtime.Object w/ metadata.
func (f *FakeOpa) keyFor(obj interface{}) (OpaKey, error) {
	o, ok := obj.(client.Object)
	if !ok {
		return OpaKey{}, fmt.Errorf("expected runtime.Object, got: %T", obj)
	}
	gvk := o.GetObjectKind().GroupVersionKind()
	ns := o.GetNamespace()
	if ns == "" {
		return OpaKey{Gvk: gvk, Key: o.GetName()}, nil
	}

	return OpaKey{Gvk: gvk, Key: fmt.Sprintf("%s/%s", ns, o.GetName())}, nil
}

func (f *FakeOpa) AddData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.needsToError {
		return nil, fmt.Errorf("test error")
	}

	key, err := f.keyFor(data)
	if err != nil {
		return nil, err
	}

	if f.data == nil {
		f.data = make(map[OpaKey]interface{})
	}

	f.data[key] = data
	return &constraintTypes.Responses{}, nil
}

func (f *FakeOpa) RemoveData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.needsToError {
		return nil, fmt.Errorf("test error")
	}

	if target.IsWipeData(data) {
		f.data = make(map[OpaKey]interface{})
		return &constraintTypes.Responses{}, nil
	}

	key, err := f.keyFor(data)
	if err != nil {
		return nil, err
	}

	delete(f.data, key)
	return &constraintTypes.Responses{}, nil
}

// Contains returns true if all expected resources are in the cache.
func (f *FakeOpa) Contains(expected map[OpaKey]interface{}) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for k := range expected {
		if _, ok := f.data[k]; !ok {
			return false
		}
	}
	return true
}

// HasGVK returns true if the cache has any data of the requested kind.
func (f *FakeOpa) HasGVK(gvk schema.GroupVersionKind) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for k := range f.data {
		if k.Gvk == gvk {
			return true
		}
	}
	return false
}

// Len returns the number of items in the cache.
func (f *FakeOpa) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data)
}

// SetErroring will error out on AddObject or RemoveObject.
func (f *FakeOpa) SetErroring(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.needsToError = enabled
}
