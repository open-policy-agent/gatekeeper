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

import "strings"

// errorList is an error that aggregates multiple errors.
type errorList []error

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
