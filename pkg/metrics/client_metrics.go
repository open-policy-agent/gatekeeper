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

package metrics

import (
	"context"
	"net/url"
	"time"

	clientmetrics "k8s.io/client-go/tools/metrics"
	_ "sigs.k8s.io/controller-runtime/pkg/metrics" // Needed for init() side effect
)

// DisableRESTClientMetrics disables the rest client latency histograms configured by
// controller-runtime in sigs.k8s.io/controller-runtime/pkg/metrics/client_go_adapter.go#registerClientMetrics.
func DisableRESTClientMetrics() {
	clientmetrics.RequestLatency = noopLatency{}
}

type noopLatency struct{}

func (noopLatency) Observe(context.Context, string, url.URL, time.Duration) {}
