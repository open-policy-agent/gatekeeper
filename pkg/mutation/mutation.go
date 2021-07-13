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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// MutationEnabled indicates if the mutation feature is enabled.
var (
	MutationEnabled            *bool
	MutationLoggingEnabled     *bool
	MutationAnnotationsEnabled *bool
	log                        = logf.Log.WithName("mutation").WithValues(logging.Process, "mutation")
)

func init() {
	MutationEnabled = flag.Bool("enable-mutation", false, "(alpha) Enable the mutation feature")
	MutationLoggingEnabled = flag.Bool("log-mutations", false, "(alpha) Enable detailed logging of mutation events")
	MutationAnnotationsEnabled = flag.Bool("mutation-annotations", false, "(alpha) Enable mutation annotations")
}
