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

package readiness

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logf.Log.WithName("readiness-tracker")

func NewTracker(mgr manager.Manager) *Tracker {
	return &Tracker{
		Manager:   mgr,
		Seen:      make(map[string]bool),
		Expecting: make(map[string]bool),
	}
}

type Tracker struct {
	mutex       sync.RWMutex
	Manager     manager.Manager
	Seen        map[string]bool
	Expecting   map[string]bool
	InitialSync bool
	Ready       bool
}

func (t *Tracker) Observe(group, kind, name, namespace string) {
	t.mutex.RLock()
	ready := t.Ready
	t.mutex.RUnlock()

	if ready {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.Seen[fmt.Sprintf("%s|%s|%s|%s", group, kind, name, namespace)] = true
}

func (t *Tracker) Expect(group, kind, name, namespace string) {
	t.mutex.RLock()
	ready := t.Ready
	t.mutex.RUnlock()

	if ready {
		return
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.Expecting[fmt.Sprintf("%s|%s|%s|%s", group, kind, name, namespace)] = true
}

func (t *Tracker) Satisfied() bool {
	t.mutex.RLock()

	if t.Ready {
		t.mutex.RUnlock()
		return true
	}

	if !t.InitialSync {
		t.mutex.RUnlock()
		return false
	}

	for k := range t.Expecting {
		if !t.Seen[k] {
			t.mutex.RUnlock()
			return false
		}
	}
	t.mutex.RUnlock()

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.Ready = true
	// release uneeded memory, not necessary, but nice to do
	t.Seen = make(map[string]bool)
	t.Expecting = make(map[string]bool)
	return true
}

func (t *Tracker) CheckSatisfied(req *http.Request) error {
	if !t.Satisfied() {
		return errors.New("expectations not satisfied")
	}
	return nil
}

func (t *Tracker) Start(done <-chan struct{}) error {
	ctx, cancel := syncutil.ContextForChannel(done)
	defer cancel()

	managerCache := t.Manager.GetCache()
	if !managerCache.WaitForCacheSync(done) {
		return errors.New("Unable to sync cache")
	}

	configGVK := schema.GroupVersionKind{
		Group:   configv1alpha1.GroupVersion.Group,
		Kind:    "Config",
		Version: configv1alpha1.GroupVersion.Version,
	}

	configInformer, err := managerCache.GetInformerForKind(ctx, configGVK)
	if err != nil {
		return err
	}

	sharedConfigInformer, ok := configInformer.(cache.SharedInformer)
	if !ok {
		return fmt.Errorf(
			"expected informer to be of type `SharedInformer` instead found `%T`",
			configInformer,
		)
	}

	for _, config := range sharedConfigInformer.GetStore().List() {
		cachedConfig, _ := config.(*configv1alpha1.Config)
		if cachedConfig.GetName() != keys.Config.Name || cachedConfig.GetNamespace() != keys.Config.Namespace {
			continue
		}

		t.Expect(
			configGVK.Group,
			configGVK.Kind,
			cachedConfig.GetName(),
			cachedConfig.GetNamespace(),
		)

		for _, entry := range cachedConfig.Spec.Sync.SyncOnly {
			gvk := schema.GroupVersionKind{
				Group:   entry.Group,
				Version: entry.Version,
				Kind:    entry.Kind,
			}
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)

			informer, err := managerCache.GetInformer(ctx, u)
			if err != nil {
				log.V(1).Info("Unable to fetch informer", "gvk", gvk)
				continue
			}
			sharedInformer, ok := informer.(cache.SharedInformer)
			if !ok {
				return fmt.Errorf(
					"expected informer to be of type `SharedInformer` instead found `%T`",
					informer,
				)
			}

			if !cache.WaitForCacheSync(done, sharedInformer.HasSynced) {
				return errors.New("Unable to sync cache")
			}

			for _, item := range sharedInformer.GetStore().List() {
				cachedObject, _ := item.(metav1.Object)
				t.Expect(
					gvk.Group,
					gvk.Kind,
					cachedObject.GetName(),
					cachedObject.GetNamespace(),
				)
			}
		}
	}

	templateGVK := schema.GroupVersionKind{
		Group:   v1beta1.SchemeGroupVersion.Group,
		Kind:    "ConstraintTemplate",
		Version: v1beta1.SchemeGroupVersion.Version,
	}

	templateInformer, err := managerCache.GetInformerForKind(ctx, templateGVK)
	if err != nil {
		return err
	}

	sharedTemplateInformer, ok := templateInformer.(cache.SharedInformer)
	if !ok {
		return fmt.Errorf(
			"expected informer to be of type `SharedInformer` instead found `%T`",
			templateInformer,
		)
	}

	for _, template := range sharedTemplateInformer.GetStore().List() {
		cachedTemplate, _ := template.(*v1beta1.ConstraintTemplate)
		t.Expect(
			templateGVK.Group,
			templateGVK.Kind,
			cachedTemplate.GetName(),
			cachedTemplate.GetNamespace(),
		)

		constraintGVK := schema.GroupVersionKind{
			Group:   "constraints.gatekeeper.sh",
			Kind:    cachedTemplate.Spec.CRD.Spec.Names.Kind,
			Version: v1beta1.SchemeGroupVersion.Version,
		}

		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(constraintGVK)
		informer, err := managerCache.GetInformer(ctx, u)
		if err != nil {
			log.V(1).Info("Unable to fetch informer", "gvk", constraintGVK)
			continue
		}

		sharedInformer, ok := informer.(cache.SharedInformer)
		if !ok {
			return fmt.Errorf(
				"expected informer to be of type `SharedInformer` instead found `%T`",
				informer,
			)
		}

		if !cache.WaitForCacheSync(done, sharedInformer.HasSynced) {
			return errors.New("Unable to sync cache")
		}

		for _, item := range sharedInformer.GetStore().List() {
			cachedObject, _ := item.(metav1.Object)
			t.Expect(
				constraintGVK.Group,
				constraintGVK.Kind,
				cachedObject.GetName(),
				cachedObject.GetNamespace(),
			)
		}
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.InitialSync = true
	return nil
}
