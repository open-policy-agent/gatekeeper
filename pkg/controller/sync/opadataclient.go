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

package sync

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/runtime"
)

// OpaDataClient is an interface for caching data.
type OpaDataClient interface {
	AddData(ctx context.Context, data interface{}) (*types.Responses, error)
	RemoveData(ctx context.Context, data interface{}) (*types.Responses, error)
}

// FilteredDataClient is an OpaDataClient which drops any unwatched resources.
type FilteredDataClient struct {
	watched *watch.Set
	opa     OpaDataClient
}

func NewFilteredOpaDataClient(opa OpaDataClient, watchSet *watch.Set) *FilteredDataClient {
	return &FilteredDataClient{
		watched: watchSet,
		opa:     opa,
	}
}

// AddData adds data to the opa cache if that data is currently being watched.
// Unwatched data is silently dropped with no error.
func (f *FilteredDataClient) AddData(ctx context.Context, data interface{}) (*types.Responses, error) {
	if obj, ok := data.(runtime.Object); ok {
		gvk := obj.GetObjectKind().GroupVersionKind()
		if !f.watched.Contains(gvk) {
			return &types.Responses{}, nil
		}
	}

	return f.opa.AddData(ctx, data)
}

// RemoveData removes data from the opa cache if that data is currently being watched.
// Unwatched data is silently dropped with no error.
func (f *FilteredDataClient) RemoveData(ctx context.Context, data interface{}) (*types.Responses, error) {
	if obj, ok := data.(runtime.Object); ok {
		gvk := obj.GetObjectKind().GroupVersionKind()
		if !f.watched.Contains(gvk) {
			return &types.Responses{}, nil
		}
	}

	return f.opa.RemoveData(ctx, data)
}
