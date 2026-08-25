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

// The predicates below name the capabilities controller setup has to decide on,
// so wiring code can ask "does this pod need X?" instead of re-deriving the
// operation set at every call site.

// NeedsValidationDataSync returns true when the pod has to keep referential
// data in sync for validation. It gates the cache manager, its watch registrar,
// the sync controller and the expectations pruner.
func NeedsValidationDataSync() bool {
	return HasValidationOperations()
}

// NeedsProcessExclusions returns true when the pod evaluates the per-process
// namespace exclusions carried by the Config resource. Generate is included
// because synced VAP enforcement scope inherits the webhook exclusions.
func NeedsProcessExclusions() bool {
	return IsAssigned(Audit) ||
		IsAssigned(Webhook) ||
		IsAssigned(MutationWebhook) ||
		IsAssigned(Generate) ||
		NeedsValidationDataSync()
}

// NeedsGenerateConfigNotifications returns true when the pod turns Config
// changes into constraint template reconciliation so that generated VAP objects
// track Gatekeeper's enforcement scope. Callers must also honor
// --sync-vap-enforcement-scope.
func NeedsGenerateConfigNotifications() bool {
	return IsAssigned(Generate)
}

// NeedsConfigReconciliation returns true when the Config controller has work to
// do: process exclusions, validation data sync, or generate notifications.
func NeedsConfigReconciliation() bool {
	return NeedsProcessExclusions() ||
		NeedsValidationDataSync() ||
		NeedsGenerateConfigNotifications()
}
