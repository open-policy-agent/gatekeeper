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

package config

import (
	"context"
	"fmt"
	gosync "sync"

	constraintTypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type opaKey struct {
	gvk schema.GroupVersionKind
	key string
}

// fakeOpa is an OpaDataClient for testing.
type fakeOpa struct {
	mu   gosync.Mutex
	data map[opaKey]interface{}
}

// keyFor returns an opaKey for the provided resource.
// Returns error if the resource is not a runtime.Object w/ metadata.
func (f *fakeOpa) keyFor(obj interface{}) (opaKey, error) {
	o, ok := obj.(runtime.Object)
	if !ok {
		return opaKey{}, fmt.Errorf("expected runtime.Object, got: %T", obj)
	}
	gvk := o.GetObjectKind().GroupVersionKind()
	k, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return opaKey{}, fmt.Errorf("extracting key: %v", err)
	}
	return opaKey{
		gvk: gvk,
		key: k,
	}, nil
}

func (f *fakeOpa) AddData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key, err := f.keyFor(data)
	if err != nil {
		return nil, err
	}

	if f.data == nil {
		f.data = make(map[opaKey]interface{})
	}

	f.data[key] = data
	return &constraintTypes.Responses{}, nil
}

func (f *fakeOpa) RemoveData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := data.(target.WipeData); ok {
		f.data = make(map[opaKey]interface{})
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
func (f *fakeOpa) Contains(expected map[opaKey]interface{}) bool {
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
func (f *fakeOpa) HasGVK(gvk schema.GroupVersionKind) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for k := range f.data {
		if k.gvk == gvk {
			return true
		}
	}
	return false
}

// Len returns the number of items in the cache.
func (f *fakeOpa) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data)
}

// hookReader is a client.Reader with overrideable methods.
type hookReader struct {
	client.Reader
	ListFunc func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

func (r hookReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if r.ListFunc != nil {
		return r.ListFunc(ctx, list, opts...)
	}
	return r.Reader.List(ctx, list, opts...)
}
