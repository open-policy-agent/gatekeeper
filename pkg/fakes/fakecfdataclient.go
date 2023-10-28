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
package fakes

import (
	"context"
	"fmt"
	gosync "sync"

	constraintTypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CfDataKey struct {
	Gvk schema.GroupVersionKind
	Key string
}

// FakeCfClient is an CfDataClient for testing.
type FakeCfClient struct {
	mu           gosync.Mutex
	data         map[CfDataKey]interface{}
	needsToError bool
}

// KeyFor returns a CfDataKey for the provided resource.
// Returns error if the resource is not a runtime.Object w/ metadata.
func KeyFor(obj interface{}) (CfDataKey, error) {
	o, ok := obj.(client.Object)
	if !ok {
		return CfDataKey{}, fmt.Errorf("expected runtime.Object, got: %T", obj)
	}
	gvk := o.GetObjectKind().GroupVersionKind()
	ns := o.GetNamespace()
	if ns == "" {
		return CfDataKey{Gvk: gvk, Key: o.GetName()}, nil
	}

	return CfDataKey{Gvk: gvk, Key: fmt.Sprintf("%s/%s", ns, o.GetName())}, nil
}

func (f *FakeCfClient) AddData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.needsToError {
		return nil, fmt.Errorf("test error")
	}

	key, err := KeyFor(data)
	if err != nil {
		return nil, err
	}

	if f.data == nil {
		f.data = make(map[CfDataKey]interface{})
	}

	f.data[key] = data
	return &constraintTypes.Responses{}, nil
}

func (f *FakeCfClient) RemoveData(ctx context.Context, data interface{}) (*constraintTypes.Responses, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.needsToError {
		return nil, fmt.Errorf("test error")
	}

	if target.IsWipeData(data) {
		f.data = make(map[CfDataKey]interface{})
		return &constraintTypes.Responses{}, nil
	}

	key, err := KeyFor(data)
	if err != nil {
		return nil, err
	}

	delete(f.data, key)
	return &constraintTypes.Responses{}, nil
}

// GetData returns data for a CfDataKey. It assumes that the
// key is present in the FakeCfClient. Also the data returned is not copied
// and it's meant only for assertions not modifications.
func (f *FakeCfClient) GetData(key CfDataKey) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.data[key]
}

// Contains returns true if all expected resources are in the cache.
func (f *FakeCfClient) Contains(expected map[CfDataKey]interface{}) bool {
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
func (f *FakeCfClient) HasGVK(gvk schema.GroupVersionKind) bool {
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
func (f *FakeCfClient) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data)
}

// SetErroring will error out on AddObject or RemoveObject.
func (f *FakeCfClient) SetErroring(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.needsToError = enabled
}

func UnstructuredFor(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	if namespace == "" {
		u.SetNamespace("default")
	} else {
		u.SetNamespace(namespace)
	}

	if gvk.Kind == "Pod" {
		u.Object["spec"] = map[string]interface{}{
			"containers": []map[string]interface{}{
				{
					"name":  "foo-container",
					"image": "foo-image",
				},
			},
		}
	}

	return u
}
