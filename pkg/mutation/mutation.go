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

package mutation

import (
	"flag"

	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	DeprecatedMutationEnabled  = flag.Bool("enable-mutation", false, "Deprecated. This used to enable the mutation feature, now it has no effect. Use --operation=mutation-webhook and --operation=mutation-status instead.")
	MutationLoggingEnabled     *bool
	MutationAnnotationsEnabled *bool
	log                        = logf.Log.WithName("mutation").WithValues(logging.Process, "mutation")
)

func init() {
	MutationLoggingEnabled = flag.Bool("log-mutations", false, "Enable detailed logging of mutation events")
	MutationAnnotationsEnabled = flag.Bool("mutation-annotations", false, "Enable mutation annotations")
}

// Enabled indicates if the mutation feature is enabled.
func Enabled() bool {
	return operations.IsAssigned(operations.MutationStatus) || operations.IsAssigned(operations.MutationWebhook)
}
