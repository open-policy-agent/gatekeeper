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

import "k8s.io/apimachinery/pkg/runtime"

// noopExpectations returns an Expectations that is always satisfied.
type noopExpectations struct {
}

func (n noopExpectations) Expect(o runtime.Object) {
}

func (n noopExpectations) CancelExpect(o runtime.Object) {
}

func (n noopExpectations) ExpectationsDone() {
}

func (n noopExpectations) Observe(o runtime.Object) {
}

func (n noopExpectations) Satisfied() bool {
	return true
}
