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

package syncutil

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CacheManagerMediator is an interface for mediating
// with a CacheManager but not actually depending on an instance.
type CacheManagerMediator interface {
	AddObject(ctx context.Context, instance *unstructured.Unstructured) error
	RemoveObject(ctx context.Context, instance *unstructured.Unstructured) error

	ReportSyncMetrics()
}

// OpaDataClient is an interface for caching data.
type OpaDataClient interface {
	AddData(ctx context.Context, data interface{}) (*types.Responses, error)
	RemoveData(ctx context.Context, data interface{}) (*types.Responses, error)
}
