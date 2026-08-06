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

package externaldata

import "sync/atomic"

// providerErrorTotal is the cumulative count of external data provider
// reconciliation errors. Shared so the stats reporter can observe it as an
// Int64ObservableCounter (always exported, including 0).
var providerErrorTotal atomic.Int64

// ReportProviderError increments the provider error counter.
func ReportProviderError() {
	providerErrorTotal.Add(1)
}

// ProviderErrorCount returns the current cumulative provider error count.
func ProviderErrorCount() int64 {
	return providerErrorTotal.Load()
}

// ResetProviderErrorCount sets the counter to zero. For tests only.
func ResetProviderErrorCount() {
	providerErrorTotal.Store(0)
}
