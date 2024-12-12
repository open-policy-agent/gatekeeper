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

package audit

import (
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Dependencies struct {
	Client          *constraintclient.Client
	ProcessExcluder *process.Excluder
	CacheLister     *CacheLister
	ExpansionSystem *expansion.System
	ExportSystem    *export.System
}

// AddToManager adds audit manager to the Manager.
func AddToManager(m manager.Manager, deps *Dependencies) error {
	if *auditInterval == 0 {
		log.Info("auditing is disabled")
		return nil
	}
	am, err := New(m, deps)
	if err != nil {
		return err
	}
	return m.Add(am)
}
