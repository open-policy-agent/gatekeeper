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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type objKey struct {
	gvk            schema.GroupVersionKind
	namespacedName types.NamespacedName
}

func (k *objKey) String() string {
	return fmt.Sprintf("%s [%s]", k.namespacedName.String(), k.gvk.String())
}

// objSet is a set of objKey types with no data.
type objSet map[objKey]struct{}

// retryObjSet holds the allowed retries for a specific object.
type objRetrySet map[objKey]objData

type objData struct {
	retries int
}

// decrementRetries handles objData retries, and returns `true` if it's time to delete the objData entry.
func (o *objData) decrementRetries() bool {
	// if retries is less than 0, allowed retries are infinite
	if o.retries < 0 {
		return false
	}

	// If we have retries left, use one
	if o.retries > 0 {
		o.retries--
		return false
	}

	// if we have zero retries, we can delete
	return true
}

type objDataFactory func() objData

func objDataFromFlags() objData {
	return objData{retries: *readinessRetries}
}
