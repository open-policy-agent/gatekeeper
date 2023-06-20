/*
Copyright 2019 The Kubernetes Authors.

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

package builder_test

import (
	"os"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	examplegroup "sigs.k8s.io/controller-runtime/examples/crd/pkg"
)

// examplegroup.ChaosPod has implemented both admission.Defaulter and
// admission.Validator interfaces.
var _ admission.Defaulter = &examplegroup.ChaosPod{}
var _ admission.Validator = &examplegroup.ChaosPod{}

// This example use webhook builder to create a simple webhook that is managed
// by a manager for CRD ChaosPod. And then start the manager.
func ExampleWebhookBuilder() {
	var log = logf.Log.WithName("webhookbuilder-example")

	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{})
	if err != nil {
		log.Error(err, "could not create manager")
		os.Exit(1)
	}

	err = builder.
		WebhookManagedBy(mgr).         // Create the WebhookManagedBy
		For(&examplegroup.ChaosPod{}). // ChaosPod is a CRD.
		Complete()
	if err != nil {
		log.Error(err, "could not create webhook")
		os.Exit(1)
	}

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "could not start manager")
		os.Exit(1)
	}
}
