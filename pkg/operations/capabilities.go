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

// NeedsValidationDataSync returns true when the pod has to keep referential
// data in sync for validation. It gates the watch manager, the cache manager
// and its registrar, the sync controller and the expectations pruner.
func NeedsValidationDataSync() bool {
	return HasValidationOperations()
}
