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
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/api/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("readiness-tracker")

const constraintGroup = "constraints.gatekeeper.sh"

var constraintTemplateGVK = v1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate")
var cfgKey = types.NamespacedName{Namespace: util.GetNamespace(), Name: "config"}

// Lister lists resources from a cache.
type Lister interface {
	List(ctx context.Context, out runtime.Object, opts ...client.ListOption) error
}

// DynamicLister lists unstructured resources using a dynamic watch manager.
type DynamicLister interface {
	List(ctx context.Context, gvk schema.GroupVersionKind, cbForEach func(runtime.Object)) error
}

type trackerMap struct {
	mu      sync.RWMutex
	m       map[schema.GroupVersionKind]*ObjectTracker
	removed map[schema.GroupVersionKind]struct{}
}

func newTrackerMap() *trackerMap {
	return &trackerMap{
		m:       make(map[schema.GroupVersionKind]*ObjectTracker),
		removed: make(map[schema.GroupVersionKind]struct{}),
	}
}

// Get returns an ObjectTracker for the requested resource kind.
// A new one is created if the resource was not previously tracked.
func (t *trackerMap) Get(gvk schema.GroupVersionKind) *ObjectTracker {
	if entry := func() *ObjectTracker {
		t.mu.RLock()
		defer t.mu.RUnlock()

		if _, ok := t.removed[gvk]; ok {
			// Return a throwaway tracker if it was previously removed.
			return newObjTracker(gvk)
		}
		return t.m[gvk]
	}(); entry != nil {
		return entry
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	entry := newObjTracker(gvk)
	t.m[gvk] = entry
	return entry
}

// Keys returns the resource kinds currently being tracked.
func (t *trackerMap) Keys() []schema.GroupVersionKind {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]schema.GroupVersionKind, 0, len(t.m))
	for k := range t.m {
		out = append(out, k)
	}
	return out
}

// Remove stops tracking a resource kind. It cannot be tracked again by the same map.
func (t *trackerMap) Remove(gvk schema.GroupVersionKind) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, gvk)
	t.removed[gvk] = struct{}{}
}

// Satisfied returns true if all tracked expectations have been satisfied.
func (t *trackerMap) Satisfied() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, ot := range t.m {
		if !ot.Satisfied() {
			return false
		}
	}
	return true
}

// Tracker tracks readiness for templates, constraints and data.
type Tracker struct {
	lister        Lister
	dynamicLister DynamicLister

	templates   *ObjectTracker
	config      *ObjectTracker
	constraints *trackerMap
	data        *trackerMap

	ready              chan struct{}
	constraintTrackers *syncutil.SingleRunner
}

func NewTracker(lister Lister, dynamicLister DynamicLister) *Tracker {
	return &Tracker{
		lister:             lister,
		dynamicLister:      dynamicLister,
		templates:          newObjTracker(v1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate")),
		config:             newObjTracker(configv1alpha1.GroupVersion.WithKind("Config")),
		constraints:        newTrackerMap(),
		data:               newTrackerMap(),
		ready:              make(chan struct{}),
		constraintTrackers: &syncutil.SingleRunner{},
	}
}

// CheckSatisfied implements healthz.Checker to report readiness based on tracker status.
// Returns nil if all expectations have been satisfied, otherwise returns an error.
func (t *Tracker) CheckSatisfied(req *http.Request) error {
	if !t.Satisfied(req.Context()) {
		return errors.New("expectations not satisfied")
	}
	return nil
}

// For returns an ObjectTracker for the requested resource kind.
func (t *Tracker) For(gvk schema.GroupVersionKind) *ObjectTracker {
	switch {
	case gvk.GroupVersion() == v1beta1.SchemeGroupVersion && gvk.Kind == "ConstraintTemplate":
		return t.templates
	case gvk.GroupVersion() == configv1alpha1.GroupVersion && gvk.Kind == "Config":
		return t.config
	default:
		return t.constraints.Get(gvk)
	}
}

// ForData returns an data ObjectTracker for the requested resource kind.
func (t *Tracker) ForData(gvk schema.GroupVersionKind) *ObjectTracker {
	return t.data.Get(gvk)
}

// CancelTemplate stops expecting the provided ConstraintTemplate and associated Constraints.
func (t *Tracker) CancelTemplate(ct *templates.ConstraintTemplate) {
	log.V(1).Info("cancel tracking for template", "namespace", ct.GetNamespace(), "name", ct.GetName())
	t.templates.CancelExpect(ct)
	gvk := constraintGVK(ct)
	t.constraints.Remove(gvk)
	<-t.ready // constraintTrackers are setup in Run()
	t.constraintTrackers.Cancel(gvk.String())
}

// CancelData stops expecting data for the specified resource kind.
func (t *Tracker) CancelData(gvk schema.GroupVersionKind) {
	t.data.Remove(gvk)
}

// Satisfied returns true if all tracked expectations have been satisfied.
func (t *Tracker) Satisfied(ctx context.Context) bool {
	return t.templates.Satisfied() && t.constraints.Satisfied() && t.config.Satisfied() && t.data.Satisfied()
}

// Run runs the tracker and blocks until it completes.
// The provided context can be cancelled to signal a shutdown request.
func (t *Tracker) Run(ctx context.Context) error {
	var grp errgroup.Group
	t.constraintTrackers = syncutil.RunnerWithContext(ctx)
	close(t.ready) // The constraintTrackers SingleRunner is ready.

	grp.Go(func() error {
		return t.trackConstraintTemplates(ctx)
	})
	grp.Go(func() error {
		return t.trackConfig(ctx)
	})

	_ = grp.Wait()
	_ = t.constraintTrackers.Wait()
	return nil
}

func (t *Tracker) trackConstraintTemplates(ctx context.Context) error {
	templates := &v1beta1.ConstraintTemplateList{}
	if err := t.lister.List(ctx, templates); err != nil {
		return fmt.Errorf("listing templates: %w", err)
	}

	handled := make(map[schema.GroupVersionKind]bool, len(templates.Items))
	for _, ct := range templates.Items {
		log.V(1).Info("[readiness] expecting ConstraintTemplate", "name", ct.GetName())
		t.templates.Expect(&ct)

		gvk := schema.GroupVersionKind{
			Group:   constraintGroup,
			Version: v1beta1.SchemeGroupVersion.Version,
			Kind:    ct.Spec.CRD.Spec.Names.Kind,
		}
		if _, ok := handled[gvk]; ok {
			log.Info("duplicate constraint type", "gvk", gvk)
			continue
		}
		handled[gvk] = true
		// Set an expectation for this constraint type
		ot := t.constraints.Get(gvk)
		t.constraintTrackers.Go(gvk.String(), func(ctx context.Context) error {
			return t.trackConstraints(ctx, gvk, ot)
		})
	}
	t.templates.ExpectationsDone()
	return nil
}

// trackConfig sets expectations for the singleton Config resource.
func (t *Tracker) trackConfig(ctx context.Context) error {
	lst := &configv1alpha1.ConfigList{}
	if err := t.lister.List(ctx, lst); err != nil {
		return fmt.Errorf("listing config: %w", err)
	}

	for _, c := range lst.Items {
		if c.GetName() != cfgKey.Name || c.GetNamespace() != cfgKey.Namespace {
			log.Info("ignoring unsupported config name", "namespace", c.GetNamespace(), "name", c.GetName())
			continue
		}
		log.V(1).Info("[readiness] expecting Config", "name", c.GetName(), "namespace", c.GetNamespace())
		t.config.Expect(&c)
	}

	t.config.ExpectationsDone()
	return nil
}

// trackConstraints sets expectations for all constraints managed by a template.
// Blocks until constraints can be listed or context is canceled.
func (t *Tracker) trackConstraints(ctx context.Context, gvk schema.GroupVersionKind, constraints *ObjectTracker) error {
	err := t.dynamicLister.List(ctx, gvk, func(o runtime.Object) {
		if o == nil {
			return
		}
		constraints.Expect(o)
		log.V(1).Info("[readiness] expecting Constraint", "name", objectName(o))
	})
	if err != nil {
		return err
	}

	constraints.ExpectationsDone()
	return nil
}

// isCT returns true if the object is a ConstraintTemplate (versioned or otherwise).
func isCT(o interface{}) bool {
	t := reflect.TypeOf(o)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	n := t.Name()
	return strings.HasSuffix(n, "ConstraintTemplate")
}

// Returns the constraint GVK that would be generated by a template.
func constraintGVK(ct *templates.ConstraintTemplate) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   constraintGroup,
		Version: v1beta1.SchemeGroupVersion.Version,
		Kind:    ct.Spec.CRD.Spec.Names.Kind,
	}
}

// objectName returns the name of a runtime.Object, or empty string on error.
func objectName(o runtime.Object) string {
	acc, err := meta.Accessor(o)
	if err != nil {
		return ""
	}
	return acc.GetName()
}
