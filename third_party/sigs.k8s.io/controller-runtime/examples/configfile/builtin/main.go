/*
Copyright 2020 The Kubernetes Authors.

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

package main

import (
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	cfg "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var scheme = runtime.NewScheme()

func init() {
	log.SetLogger(zap.New())
	clientgoscheme.AddToScheme(scheme)
}

func main() {
	entryLog := log.Log.WithName("entrypoint")

	// Setup a Manager
	entryLog.Info("setting up manager")
	mgr, err := ctrl.NewManager(config.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	}.AndFromOrDie(cfg.File()))
	if err != nil {
		entryLog.Error(err, "unable to set up overall controller manager")
		os.Exit(1)
	}

	// Setup a new controller to reconcile ReplicaSets
	err = ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.ReplicaSet{}).
		Owns(&corev1.Pod{}).
		Complete(&reconcileReplicaSet{
			client: mgr.GetClient(),
		})
	if err != nil {
		entryLog.Error(err, "unable to create controller")
		os.Exit(1)
	}

	entryLog.Info("starting manager")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		entryLog.Error(err, "unable to run manager")
		os.Exit(1)
	}
}
