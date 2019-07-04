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
	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var CreateWebhookFuncs []func(manager.Manager, client.Client) (webhook.Webhook, error)

// AddToManager adds all Controllers to the Manager
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
func AddToManager(m manager.Manager, opa client.Client) error {

	s, err := InitializeServer(m)
	if err != nil {
		return err
	}

	var webhooks []webhook.Webhook
	for _, createWH := range CreateWebhookFuncs {
		wh, err := createWH(m, opa)
		if err != nil {
			return err
		}
		webhooks = append(webhooks, wh)
	}
	s.Register(webhooks...)

	return nil
}
