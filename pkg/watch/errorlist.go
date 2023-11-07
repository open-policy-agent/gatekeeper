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
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type gvkErr struct {
	err      error
	gvk      schema.GroupVersionKind
	isRemove bool
}

func (w gvkErr) String() string {
	return w.Error()
}

func (w gvkErr) Error() string {
	return fmt.Sprintf("error for gvk: %s: %s", w.gvk, w.err.Error())
}

// ErrorList is an error that aggregates multiple errors.
type ErrorList struct {
	errs          []error
	hasGeneralErr bool
}

func NewErrorList() *ErrorList {
	return &ErrorList{
		errs: []error{},
	}
}

func (e *ErrorList) String() string {
	return e.Error()
}

func (e *ErrorList) Error() string {
	var builder strings.Builder
	for i, err := range e.errs {
		if i > 0 {
			builder.WriteRune('\n')
		}
		builder.WriteString(err.Error())
	}
	return builder.String()
}

// Return gvks for which there were errors adding watches.
func (e *ErrorList) AddGVKFailures() []schema.GroupVersionKind {
	gvks := []schema.GroupVersionKind{}
	for _, err := range e.errs {
		var gvkErr gvkErr
		if errors.As(err, &gvkErr) && !gvkErr.isRemove {
			gvks = append(gvks, gvkErr.gvk)
		}
	}

	return gvks
}

// Return gvks for which there were errors removing watches.
func (e *ErrorList) RemoveGVKFailures() []schema.GroupVersionKind {
	gvks := []schema.GroupVersionKind{}
	for _, err := range e.errs {
		var gvkErr gvkErr
		if errors.As(err, &gvkErr) && gvkErr.isRemove {
			gvks = append(gvks, gvkErr.gvk)
		}
	}

	return gvks
}

func (e *ErrorList) HasGeneralErr() bool {
	return e.hasGeneralErr
}

// adds a non gvk specific error to the list.
func (e *ErrorList) Err(err error) {
	e.errs = append(e.errs, err)
	e.hasGeneralErr = true
}

// adds a gvk specific error for failing to add a gvk watch to the list.
func (e *ErrorList) AddGVKErr(gvk schema.GroupVersionKind, err error) {
	e.errs = append(e.errs, gvkErr{gvk: gvk, err: err})
}

// adds a gvk specific error for failing to remove a gvk watch to the list.
func (e *ErrorList) RemoveGVKErr(gvk schema.GroupVersionKind, err error) {
	e.errs = append(e.errs, gvkErr{gvk: gvk, err: err, isRemove: true})
}

func (e *ErrorList) Size() int {
	return len(e.errs)
}
