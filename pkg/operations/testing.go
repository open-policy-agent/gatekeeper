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

package operations

// AssignForTest replaces the assigned operations with ops and returns a
// function that restores the previous set. The --operation flag is write-once
// per process, so tests covering more than one operation set need this.
func AssignForTest(ops ...Operation) func() {
	operationsMtx.Lock()
	defer operationsMtx.Unlock()

	previous := operations

	assigned := newOperationSet()
	assigned.assignedOperations = make(map[Operation]bool)
	assigned.initialized = true
	for _, op := range ops {
		assigned.assignedOperations[op] = true
	}
	operations = assigned

	return func() {
		operationsMtx.Lock()
		defer operationsMtx.Unlock()

		operations = previous
	}
}
