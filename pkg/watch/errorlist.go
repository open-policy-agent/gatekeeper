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
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// errorList is an error that aggregates multiple errors.
type errorList []GVKError

type GVKError struct {
	err error
	gvk schema.GroupVersionKind
}

func (w GVKError) String() string {
	return w.Error()
}

func (w GVKError) Error() string {
	return fmt.Sprintf("error for gvk: %s: %s", w.gvk, w.err.Error())
}

func (e errorList) String() string {
	return e.Error()
}

func (e errorList) Error() string {
	var builder strings.Builder
	for i, err := range e {
		if i > 0 {
			builder.WriteRune('\n')
		}
		builder.WriteString(err.Error())
	}
	return builder.String()
}

func (e errorList) FailingGVKs() []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, len(e))
	for _, err := range e {
		gvks = append(gvks, err.gvk)
	}

	return gvks
}
