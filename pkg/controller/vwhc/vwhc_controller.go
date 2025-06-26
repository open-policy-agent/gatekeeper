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

package vwhc

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	GatekeeperWebhookLabel = "gatekeeper.sh/system"
)

var log = logf.Log.WithName("controller").WithValues("metaKind", "ValidatingWebhookConfiguration")

var EnableDeleteOpsInVwhc *bool

type Adder struct{}

// Add creates a new vwhc Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func (a *Adder) Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler.
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileVWHC{
		reader: mgr.GetCache(),
		scheme: mgr.GetScheme(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler.
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("vwhc-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the provided resource
	return c.Watch(
		source.Kind(mgr.GetCache(), &admissionregistrationv1.ValidatingWebhookConfiguration{}, handler.TypedEnqueueRequestsFromMapFunc(reconcileWebhookMapFunc())),
	)
}

var _ reconcile.Reconciler = &ReconcileVWHC{}

// ReconcileWH reconciles a validatingwebhookconfiguration.
type ReconcileVWHC struct {
	reader client.Reader
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for an object and makes changes based on the state read
// and what is in the constraint.Spec.
func (r *ReconcileVWHC) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	vwhc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := r.reader.Get(ctx, request.NamespacedName, vwhc); err != nil {
		if k8sErrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}
	for i := range vwhc.Webhooks {
		webhook := &vwhc.Webhooks[i]
		for _, rule := range webhook.Rules {
			if containsDelete(rule.Operations) {
				log.Info("delete operation enabled in gatekeeper vwhc")
				EnableDeleteOpsInVwhc = new(bool)
				*EnableDeleteOpsInVwhc = true
				return reconcile.Result{}, nil
			}
		}
	}
	log.Info("delete operation not enabled in gatekeeper vwhc")
	EnableDeleteOpsInVwhc = new(bool)
	*EnableDeleteOpsInVwhc = false
	return reconcile.Result{}, nil
}

func reconcileWebhookMapFunc() func(ctx context.Context, object *admissionregistrationv1.ValidatingWebhookConfiguration) []reconcile.Request {
	return func(_ context.Context, object *admissionregistrationv1.ValidatingWebhookConfiguration) []reconcile.Request {
		//only watch gatekeeper validatingwebhookconfiguration
		if object.GetName() != *webhook.VwhName {
			return nil
		}
		labels := object.GetLabels()
		lv, ok := labels[GatekeeperWebhookLabel]
		if !ok {
			return nil
		}
		if lv != "yes" {
			return nil
		}
		log.Info("gatekeeper vwhc changes", "object", object.GetName(), "namespace", object.GetNamespace())
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: object.GetNamespace(),
					Name:      object.GetName(),
				},
			},
		}
	}
}

func containsDelete(ops []admissionregistrationv1.OperationType) bool {
	for _, op := range ops {
		if op == admissionregistrationv1.Delete {
			return true
		}
	}
	return false
}
