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

package webhook

import (
	"context"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Dependencies struct {
	OpaClient       *constraintclient.Client
	ProcessExcluder *process.Excluder
	MutationSystem  *mutation.System
	ExpansionSystem *expansion.System
	ExportSystem    export.Exporter
	GetPod          func(context.Context) (*corev1.Pod, error)
}

// AddToManagerFuncs is a list of functions to add all Controllers to the Manager.
var AddToManagerFuncs []func(manager.Manager, Dependencies) error

// The below autogen directive is currently disabled because controller-gen has
// no way of specifying the resource name restriction
// DISABLED +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups="",namespace=gatekeeper-system,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",namespace=gatekeeper-system,resources=events,verbs=create;patch

// AddToManager adds all Controllers to the Manager.
func AddToManager(m manager.Manager, deps Dependencies) error {
	for _, f := range AddToManagerFuncs {
		if err := f(m, deps); err != nil {
			return err
		}
	}
	return nil
}
