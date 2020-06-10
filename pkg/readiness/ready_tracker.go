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
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("readiness-tracker")

const constraintGroup = "constraints.gatekeeper.sh"

// Lister lists resources from a cache.
type Lister interface {
	List(ctx context.Context, out runtime.Object, opts ...client.ListOption) error
}

// Tracker tracks readiness for templates, constraints and data.
type Tracker struct {
	mu        sync.RWMutex // protects "satisfied" circuit-breaker
	satisfied bool         // indicates whether tracker has been satisfied at least once

	lister Lister

	templates   *objectTracker
	config      *objectTracker
	constraints *trackerMap
	data        *trackerMap

	ready              chan struct{}
	constraintTrackers *syncutil.SingleRunner
}

func NewTracker(lister Lister) *Tracker {
	return &Tracker{
		lister:             lister,
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

// For returns Expectations for the requested resource kind.
func (t *Tracker) For(gvk schema.GroupVersionKind) Expectations {
	switch {
	case gvk.GroupVersion() == v1beta1.SchemeGroupVersion && gvk.Kind == "ConstraintTemplate":
		return t.templates
	case gvk.GroupVersion() == configv1alpha1.GroupVersion && gvk.Kind == "Config":
		return t.config
	}

	// Avoid new constraint trackers after templates have been populated.
	// Race is ok here - extra trackers will only consume some unneeded memory.
	if t.templates.Populated() && !t.constraints.Has(gvk) {
		// Return throw-away tracker instead.
		return noopExpectations{}
	}

	return t.constraints.Get(gvk)
}

// ForData returns Expectations for tracking data of the requested resource kind.
func (t *Tracker) ForData(gvk schema.GroupVersionKind) Expectations {
	// Avoid new data trackers after data expectations have been fully populated.
	// Race is ok here - extra trackers will only consume some unneeded memory.
	if t.config.Populated() && !t.data.Has(gvk) {
		// Return throw-away tracker instead.
		return noopExpectations{}
	}
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
	log.V(1).Info("cancel tracking for data", "gvk", gvk)
	t.data.Remove(gvk)
}

// Satisfied returns true if all tracked expectations have been satisfied.
func (t *Tracker) Satisfied(ctx context.Context) bool {
	// Check circuit-breaker first. Once satisfied, always satisfied.
	t.mu.RLock()
	satisfied := t.satisfied
	t.mu.RUnlock()
	if satisfied {
		return true
	}

	if !t.templates.Satisfied() {
		return false
	}
	templateKinds := t.templates.kinds()
	for _, gvk := range templateKinds {
		if !t.constraints.Get(gvk).Satisfied() {
			return false
		}
	}

	if !t.config.Satisfied() {
		return false
	}
	configKinds := t.config.kinds()
	for _, gvk := range configKinds {
		if !t.data.Get(gvk).Satisfied() {
			return false
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.satisfied = true
	return true
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
	return nil
}

func (t *Tracker) trackConstraintTemplates(ctx context.Context) error {
	defer func() {
		t.templates.ExpectationsDone()
		log.V(1).Info("template expectations populated")

		_ = t.constraintTrackers.Wait()
	}()

	templates := &v1beta1.ConstraintTemplateList{}
	lister := retryLister(t.lister, retryAll)
	if err := lister.List(ctx, templates); err != nil {
		return fmt.Errorf("listing templates: %w", err)
	}

	log.V(1).Info("setting expectations for templates", "templateCount", len(templates.Items))

	handled := make(map[schema.GroupVersionKind]bool, len(templates.Items))
	for _, ct := range templates.Items {
		log.V(1).Info("expecting template", "name", ct.GetName())
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
			err := t.trackConstraints(ctx, gvk, ot)
			if err != nil {
				log.Error(err, "aborted trackConstraints", "gvk", gvk)
			}
			return nil // do not return an error, this would abort other constraint trackers!
		})
	}
	return nil
}

// trackConfig sets expectations for cached data as specified by the singleton Config resource.
// Fails-open if the Config resource cannot be fetched or does not exist.
func (t *Tracker) trackConfig(ctx context.Context) error {
	var wg sync.WaitGroup
	defer func() {
		defer t.config.ExpectationsDone()
		log.V(1).Info("config expectations populated")

		wg.Wait()
	}()

	cfg, err := t.getConfigResource(ctx)
	if err != nil {
		return fmt.Errorf("fetching config resource: %w", err)
	}
	if cfg == nil {
		log.Info("config resource not found - skipping for readiness")
		return nil
	}
	if !cfg.GetDeletionTimestamp().IsZero() {
		log.Info("config resource is being deleted - skipping for readiness")
		return nil
	}

	// Expect the resource kinds specified in the Config.
	// We will fail-open (resolve expectations) for GVKs
	// that are unregistered.
	for _, entry := range cfg.Spec.Sync.SyncOnly {
		gvk := schema.GroupVersionKind{
			Group:   entry.Group,
			Version: entry.Version,
			Kind:    entry.Kind,
		}
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)
		t.config.Expect(u)
		t.config.Observe(u) // we only care about the gvk entry in kinds()

		// Set expectations for individual cached resources
		dt := t.ForData(gvk)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := t.trackData(ctx, gvk, dt)
			if err != nil {
				log.Error(err, "aborted trackData", "gvk", gvk)
			}
		}()
	}

	return nil
}

// getConfigResource returns the Config singleton if present.
// Returns a nil reference if it is not found.
func (t *Tracker) getConfigResource(ctx context.Context) (*configv1alpha1.Config, error) {
	lst := &configv1alpha1.ConfigList{}
	lister := retryLister(t.lister, nil)
	if err := lister.List(ctx, lst); err != nil {
		return nil, fmt.Errorf("listing config: %w", err)
	}

	for _, c := range lst.Items {
		if c.GetName() != keys.Config.Name || c.GetNamespace() != keys.Config.Namespace {
			log.Info("ignoring unsupported config name", "namespace", c.GetNamespace(), "name", c.GetName())
			continue
		}
		return &c, nil
	}

	// Not found.
	return nil, nil
}

// trackData sets expectations for all cached data expected by Gatekeeper.
// If the provided gvk is registered, blocks until data can be listed or context is canceled.
// Invalid GVKs (not registered to the RESTMapper) will fail-open.
func (t *Tracker) trackData(ctx context.Context, gvk schema.GroupVersionKind, dt Expectations) error {
	defer func() {
		dt.ExpectationsDone()
		log.V(1).Info("data expectations populated", "gvk", gvk)
	}()

	// List individual resources and expect observations of each in the sync controller.
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})
	// NoKindMatchError is non-recoverable, otherwise we'll retry.
	lister := retryLister(t.lister, retryUnlessUnregistered)
	err := lister.List(ctx, u)
	if err != nil {
		log.Error(err, "listing data", "gvk", gvk)
		return err
	}

	for i := range u.Items {
		item := &u.Items[i]
		dt.Expect(item)
		log.V(1).Info("expecting data", "gvk", item.GroupVersionKind(), "namespace", item.GetNamespace(), "name", item.GetName())
	}
	return nil
}

// trackConstraints sets expectations for all constraints managed by a template.
// Blocks until constraints can be listed or context is canceled.
func (t *Tracker) trackConstraints(ctx context.Context, gvk schema.GroupVersionKind, constraints Expectations) error {
	defer func() {
		constraints.ExpectationsDone()
		log.V(1).Info("constraint expectations populated", "gvk", gvk)
	}()

	u := unstructured.UnstructuredList{}
	u.SetGroupVersionKind(gvk)
	lister := retryLister(t.lister, retryAll)
	if err := lister.List(ctx, &u); err != nil {
		return err
	}

	for i := range u.Items {
		o := u.Items[i]
		constraints.Expect(&o)
		log.V(1).Info("expecting Constraint", "gvk", gvk, "name", objectName(&o))
	}

	return nil
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
