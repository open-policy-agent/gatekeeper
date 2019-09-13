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
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Injector interface {
	InjectOpa(*opa.Client)
	InjectWatchManager(*watch.WatchManager)
	Add(mgr manager.Manager) error
}

// Injectors is a list of adder structs that need injection. We can convert this
// to an interface once we create controllers for things like data sync
var Injectors []Injector

// AddToManagerFuncs is a list of functions to add all Controllers to the Manager
var AddToManagerFuncs []func(manager.Manager) error

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager, client *opa.Client, wm *watch.WatchManager) error {
	for _, a := range Injectors {
		a.InjectOpa(client)
		a.InjectWatchManager(wm)
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
