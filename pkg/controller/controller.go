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

package controller

import (
	"context"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Injector interface {
	InjectOpa(*opa.Client)
	InjectWatchManager(*watch.Manager)
	InjectControllerSwitch(*watch.ControllerSwitch)
	InjectTracker(tracker *readiness.Tracker)
	Add(mgr manager.Manager) error
}

type GetPodInjector interface {
	InjectGetPod(func() (*corev1.Pod, error))
}

type GetProcessExcluderInjector interface {
	InjectProcessExcluder(processExcluder *process.Excluder)
}

// Injectors is a list of adder structs that need injection. We can convert this
// to an interface once we create controllers for things like data sync
var Injectors []Injector

// AddToManagerFuncs is a list of functions to add all Controllers to the Manager
var AddToManagerFuncs []func(manager.Manager) error

// Dependencies are dependencies that can be injected into controllers.
type Dependencies struct {
	Opa              *opa.Client
	WatchManger      *watch.Manager
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
	GetPod           func() (*corev1.Pod, error)
	ProcessExcluder  *process.Excluder
}

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager, deps Dependencies) error {
	// Reset cache on start - this is to allow for the future possibility that the OPA cache is stored remotely
	if err := deps.Opa.Reset(context.Background()); err != nil {
		return err
	}
	for _, a := range Injectors {
		a.InjectOpa(deps.Opa)
		a.InjectWatchManager(deps.WatchManger)
		a.InjectControllerSwitch(deps.ControllerSwitch)
		a.InjectTracker(deps.Tracker)
		if a2, ok := a.(GetPodInjector); ok {
			a2.InjectGetPod(deps.GetPod)
		}
		if a2, ok := a.(GetProcessExcluderInjector); ok {
			a2.InjectProcessExcluder(deps.ProcessExcluder)
		}
		if err := a.Add(m); err != nil {
			return err
		}
	}
	for _, f := range AddToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}
	return nil
}
