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
	"fmt"
	"net/http"
	"sync"
	"time"

	externaldatav1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	mutationv1beta1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("readiness-tracker")

const (
	constraintGroup = "constraints.gatekeeper.sh"
	statsPeriod     = 1 * time.Second
)

// Lister lists resources from a cache.
type Lister interface {
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

// Tracker tracks readiness for templates, constraints and data.
type Tracker struct {
	mu        sync.RWMutex // protects "satisfied" circuit-breaker
	satisfied bool         // indicates whether tracker has been satisfied at least once

	lister Lister

	templates            *objectTracker
	config               *objectTracker
	assignMetadata       *objectTracker
	assign               *objectTracker
	modifySet            *objectTracker
	externalDataProvider *objectTracker
	constraints          *trackerMap
	data                 *trackerMap

	ready               chan struct{}
	constraintTrackers  *syncutil.SingleRunner
	statsEnabled        syncutil.SyncBool
	mutationEnabled     bool
	externalDataEnabled bool
}

// NewTracker creates a new Tracker and initializes the internal trackers.
func NewTracker(lister Lister, mutationEnabled bool, externalDataEnabled bool) *Tracker {
	return newTracker(lister, mutationEnabled, externalDataEnabled, nil)
}

func newTracker(lister Lister, mutationEnabled bool, externalDataEnabled bool, fn objDataFactory) *Tracker {
	tracker := Tracker{
		lister:             lister,
		templates:          newObjTracker(v1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate"), fn),
		config:             newObjTracker(configv1alpha1.GroupVersion.WithKind("Config"), fn),
		constraints:        newTrackerMap(fn),
		data:               newTrackerMap(fn),
		ready:              make(chan struct{}),
		constraintTrackers: &syncutil.SingleRunner{},

		mutationEnabled:     mutationEnabled,
		externalDataEnabled: externalDataEnabled,
	}
	if mutationEnabled {
		tracker.assignMetadata = newObjTracker(mutationv1beta1.GroupVersion.WithKind("AssignMetadata"), fn)
		tracker.assign = newObjTracker(mutationv1beta1.GroupVersion.WithKind("Assign"), fn)
		tracker.modifySet = newObjTracker(mutationv1beta1.GroupVersion.WithKind("ModifySet"), fn)
	}
	if externalDataEnabled {
		tracker.externalDataProvider = newObjTracker(externaldatav1alpha1.SchemeGroupVersion.WithKind("Provider"), fn)
	}
	return &tracker
}

// CheckSatisfied implements healthz.Checker to report readiness based on tracker status.
// Returns nil if all expectations have been satisfied, otherwise returns an error.
func (t *Tracker) CheckSatisfied(_ *http.Request) error {
	if !t.Satisfied() {
		return errors.New("expectations not satisfied")
	}
	return nil
}

// For returns Expectations for the requested resource kind.
func (t *Tracker) For(gvk schema.GroupVersionKind) Expectations {
	if t == nil {
		return noopExpectations{}
	}

	switch {
	case gvk.GroupVersion() == v1beta1.SchemeGroupVersion && gvk.Kind == "ConstraintTemplate":
		if operations.HasValidationOperations() {
			return t.templates
		}
		return noopExpectations{}
	case gvk.GroupVersion() == configv1alpha1.GroupVersion && gvk.Kind == "Config":
		return t.config
	case gvk.GroupVersion() == externaldatav1alpha1.SchemeGroupVersion && gvk.Kind == "Provider":
		return t.externalDataProvider
	case gvk.GroupVersion() == mutationv1beta1.GroupVersion && gvk.Kind == "AssignMetadata":
		if t.mutationEnabled {
			return t.assignMetadata
		}
		return noopExpectations{}
	case gvk.GroupVersion() == mutationv1beta1.GroupVersion && gvk.Kind == "Assign":
		if t.mutationEnabled {
			return t.assign
		}
		return noopExpectations{}
	case gvk.GroupVersion() == mutationv1beta1.GroupVersion && gvk.Kind == "ModifySet":
		if t.mutationEnabled {
			return t.modifySet
		}
		return noopExpectations{}
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

func (t *Tracker) templateCleanup(ct *templates.ConstraintTemplate) {
	gvk := constraintGVK(ct)
	t.constraints.Remove(gvk)
	<-t.ready // constraintTrackers are setup in Run()
	t.constraintTrackers.Cancel(gvk.String())
}

// CancelTemplate stops expecting the provided ConstraintTemplate and associated Constraints.
func (t *Tracker) CancelTemplate(ct *templates.ConstraintTemplate) {
	log.V(1).Info("cancel tracking for template", "namespace", ct.GetNamespace(), "name", ct.GetName())
	t.templates.CancelExpect(ct)
	t.templateCleanup(ct)
}

// TryCancelTemplate will check the readiness retries left on a CT and
// cancel the expectation for that CT and its associated Constraints if
// no retries remain.
func (t *Tracker) TryCancelTemplate(ct *templates.ConstraintTemplate) {
	log.V(1).Info("try to cancel tracking for template", "namespace", ct.GetNamespace(), "name", ct.GetName())
	if t.templates.TryCancelExpect(ct) {
		t.templateCleanup(ct)
	}
}

// CancelData stops expecting data for the specified resource kind.
func (t *Tracker) CancelData(gvk schema.GroupVersionKind) {
	log.V(1).Info("cancel tracking for data", "gvk", gvk)
	t.data.Remove(gvk)
}

// Satisfied returns true if all tracked expectations have been satisfied.
func (t *Tracker) Satisfied() bool {
	// Check circuit-breaker first. Once satisfied, always satisfied.
	t.mu.RLock()
	satisfied := t.satisfied
	t.mu.RUnlock()
	if satisfied {
		return true
	}

	if t.mutationEnabled {
		if !t.assignMetadata.Satisfied() || !t.assign.Satisfied() || !t.modifySet.Satisfied() {
			return false
		}
		log.V(1).Info("all expectations satisfied", "tracker", "assignMetadata")
		log.V(1).Info("all expectations satisfied", "tracker", "assign")
		log.V(1).Info("all expectations satisfied", "tracker", "modifySet")
	}

	if t.externalDataEnabled {
		if !t.externalDataProvider.Satisfied() {
			return false
		}
		log.V(1).Info("all expectations satisfied", "tracker", "provider")
	}

	if operations.HasValidationOperations() {
		if !t.templates.Satisfied() {
			return false
		}
		templateKinds := t.templates.kinds()
		for _, gvk := range templateKinds {
			if !t.constraints.Get(gvk).Satisfied() {
				return false
			}
		}
	}
	log.V(1).Info("all expectations satisfied", "tracker", "constraints")

	if !t.config.Satisfied() {
		return false
	}

	if operations.HasValidationOperations() {
		configKinds := t.config.kinds()
		for _, gvk := range configKinds {
			if !t.data.Get(gvk).Satisfied() {
				return false
			}
		}
	}
	log.V(1).Info("all expectations satisfied", "tracker", "data")

	t.mu.Lock()
	defer t.mu.Unlock()
	t.satisfied = true
	return true
}

// Run runs the tracker and blocks until it completes.
// The provided context can be canceled to signal a shutdown request.
func (t *Tracker) Run(ctx context.Context) error {
	// Any failure in the errgroup will cancel goroutines in the group using gctx.
	// The odd one out is the statsPrinter which is meant to outlive the tracking
	// routines.
	grp, gctx := errgroup.WithContext(ctx)
	t.constraintTrackers = syncutil.RunnerWithContext(gctx)
	close(t.ready) // The constraintTrackers SingleRunner is ready.

	if t.mutationEnabled {
		grp.Go(func() error {
			return t.trackAssignMetadata(gctx)
		})
		grp.Go(func() error {
			return t.trackAssign(gctx)
		})
		grp.Go(func() error {
			return t.trackModifySet(gctx)
		})
	}
	if t.externalDataEnabled {
		grp.Go(func() error {
			return t.trackExternalDataProvider(gctx)
		})
	}
	if operations.HasValidationOperations() {
		grp.Go(func() error {
			return t.trackConstraintTemplates(gctx)
		})
	}
	grp.Go(func() error {
		return t.trackConfig(gctx)
	})
	grp.Go(func() error {
		t.statsPrinter(ctx)
		return nil
	})

	// start deleted object polling. Periodically collects
	// objects that are expected by the Tracker, but are deleted
	grp.Go(func() error {
		// wait before proceeding, hoping
		// that the tracker will be satisfied by then
		timer := time.NewTimer(2000 * time.Millisecond)
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if t.Satisfied() {
					log.Info("readiness satisfied, no further collection")
					ticker.Stop()
					return nil
				}
				t.collectInvalidExpectations(ctx)
			}
		}
	})

	_ = grp.Wait()
	_ = t.constraintTrackers.Wait() // Must appear after grp.Wait() - allows trackConstraintTemplates() time to schedule its sub-tasks.
	return nil
}

func (t *Tracker) Populated() bool {
	mutationPopulated := true
	if t.mutationEnabled {
		// If !t.mutationEnabled and we call this, it yields a null pointer exception
		mutationPopulated = t.assignMetadata.Populated() && t.assign.Populated() && t.modifySet.Populated()
	}
	externalDataProviderPopulated := true
	if t.externalDataEnabled {
		// If !t.externalDataEnabled and we call this, it yields a null pointer exception
		externalDataProviderPopulated = t.externalDataProvider.Populated()
	}
	validationPopulated := true
	if operations.HasValidationOperations() {
		validationPopulated = t.templates.Populated() && t.constraints.Populated() && t.data.Populated()
	}
	return validationPopulated && t.config.Populated() && mutationPopulated && externalDataProviderPopulated
}

// collectForObjectTracker identifies objects that are unsatisfied for the provided
// `es`, which must be an objectTracker, and removes those expectations.
func (t *Tracker) collectForObjectTracker(ctx context.Context, es Expectations, cleanup func(schema.GroupVersionKind)) error {
	if es == nil {
		return fmt.Errorf("nil Expectations provided to collectForObjectTracker")
	}

	if !es.Populated() || es.Satisfied() {
		log.V(1).Info("Expectations unpopulated or already satisfied, skipping collection")
		return nil
	}

	// es must be an objectTracker so we can fetch `unsatisfied` expectations and get GVK
	ot, ok := es.(*objectTracker)
	if !ok {
		return fmt.Errorf("expectations was not an objectTracker in collectForObjectTracker")
	}

	// there is only ever one GVK for the unsatisfied expectations of a particular objectTracker.
	gvk := ot.gvk
	ul := &unstructured.UnstructuredList{}
	ul.SetGroupVersionKind(gvk)
	lister := retryLister(t.lister, retryUnlessUnregistered)
	if err := lister.List(ctx, ul); err != nil {
		return errors.Wrapf(err, "while listing %v in collectForObjectTracker", gvk)
	}

	// identify objects in `unsatisfied` that were not found above.
	// The expectations for these objects should be collected since the
	// tracker is waiting for them, but they no longer exist.
	unsatisfied := ot.unsatisfied()
	unsatisfiedmap := make(map[objKey]struct{})
	for _, o := range unsatisfied {
		unsatisfiedmap[o] = struct{}{}
	}
	for _, o := range ul.Items {
		o := o
		k, err := objKeyFromObject(&o)
		if err != nil {
			return errors.Wrapf(err, "while getting key for %v in collectForObjectTracker", o)
		}
		// delete is a no-op if the key isn't found
		delete(unsatisfiedmap, k)
	}

	// now remove the expectations for deleted objects
	for k := range unsatisfiedmap {
		u := &unstructured.Unstructured{}
		u.SetName(k.namespacedName.Name)
		u.SetNamespace(k.namespacedName.Namespace)
		u.SetGroupVersionKind(k.gvk)
		ot.CancelExpect(u)
		if cleanup != nil {
			cleanup(gvk)
		}
	}

	return nil
}

// collectInvalidExpectations searches for any unsatisfied expectations
// for this tracker for which the expected object has been deleted, and
// cancels those expectations.
// Errors are handled and logged, but do not block collection for other trackers.
func (t *Tracker) collectInvalidExpectations(ctx context.Context) {
	tt := t.templates
	cleanupTemplate := func(gvk schema.GroupVersionKind) {
		// note that this GVK is already the GVK of the constraint
		t.constraints.Remove(gvk)
		t.constraintTrackers.Cancel(gvk.String())
	}
	err := t.collectForObjectTracker(ctx, tt, cleanupTemplate)
	if err != nil {
		log.Error(err, "while collecting for the ConstraintTemplate tracker")
	}

	ct := t.config
	cleanupData := func(gvk schema.GroupVersionKind) {
		t.data.Remove(gvk)
	}
	err = t.collectForObjectTracker(ctx, ct, cleanupData)
	if err != nil {
		log.Error(err, "while collecting for the Config tracker")
	}

	// collect deleted but expected constraints
	for _, gvk := range t.constraints.Keys() {
		// retrieve the expectations for this key
		es := t.constraints.Get(gvk)
		err = t.collectForObjectTracker(ctx, es, nil)
		if err != nil {
			log.Error(err, "while collecting for the Constraint type", "gvk", gvk)
			continue
		}
	}

	// collect data expects
	for _, gvk := range t.data.Keys() {
		// retrieve the expectations for this key
		es := t.data.Get(gvk)
		err = t.collectForObjectTracker(ctx, es, nil)
		if err != nil {
			log.Error(err, "while collecting for the Data type", "gvk", gvk)
			continue
		}
	}
}

func (t *Tracker) trackAssignMetadata(ctx context.Context) error {
	defer func() {
		t.assignMetadata.ExpectationsDone()
		log.V(1).Info("AssignMetadata expectations populated")

		_ = t.constraintTrackers.Wait()
	}()

	if !t.mutationEnabled {
		return nil
	}

	assignMetadataList := &mutationv1beta1.AssignMetadataList{}
	lister := retryLister(t.lister, retryAll)
	if err := lister.List(ctx, assignMetadataList); err != nil {
		return fmt.Errorf("listing AssignMetadata: %w", err)
	}
	log.V(1).Info("setting expectations for AssignMetadata", "AssignMetadata Count", len(assignMetadataList.Items))

	for index := range assignMetadataList.Items {
		log.V(1).Info("expecting AssignMetadata", "name", assignMetadataList.Items[index].GetName())
		t.assignMetadata.Expect(&assignMetadataList.Items[index])
	}
	return nil
}

func (t *Tracker) trackAssign(ctx context.Context) error {
	defer func() {
		t.assign.ExpectationsDone()
		log.V(1).Info("Assign expectations populated")
		_ = t.constraintTrackers.Wait()
	}()

	if !t.mutationEnabled {
		return nil
	}

	assignList := &mutationv1beta1.AssignList{}
	lister := retryLister(t.lister, retryAll)
	if err := lister.List(ctx, assignList); err != nil {
		return fmt.Errorf("listing Assign: %w", err)
	}
	log.V(1).Info("setting expectations for Assign", "Assign Count", len(assignList.Items))

	for index := range assignList.Items {
		log.V(1).Info("expecting Assign", "name", assignList.Items[index].GetName())
		t.assign.Expect(&assignList.Items[index])
	}
	return nil
}

func (t *Tracker) trackModifySet(ctx context.Context) error {
	defer func() {
		t.modifySet.ExpectationsDone()
		log.V(1).Info("ModifySet expectations populated")
		_ = t.constraintTrackers.Wait()
	}()

	if !t.mutationEnabled {
		return nil
	}

	modifySetList := &mutationv1beta1.ModifySetList{}
	lister := retryLister(t.lister, retryAll)
	if err := lister.List(ctx, modifySetList); err != nil {
		return fmt.Errorf("listing ModifySet: %w", err)
	}
	log.V(1).Info("setting expectations for ModifySet", "ModifySet Count", len(modifySetList.Items))

	for index := range modifySetList.Items {
		log.V(1).Info("expecting ModifySet", "name", modifySetList.Items[index].GetName())
		t.modifySet.Expect(&modifySetList.Items[index])
	}
	return nil
}

func (t *Tracker) trackExternalDataProvider(ctx context.Context) error {
	defer func() {
		t.externalDataProvider.ExpectationsDone()
		log.V(1).Info("Provider expectations populated")
		_ = t.constraintTrackers.Wait()
	}()

	if !t.externalDataEnabled {
		return nil
	}

	providerList := &externaldatav1alpha1.ProviderList{}
	lister := retryLister(t.lister, retryAll)
	if err := lister.List(ctx, providerList); err != nil {
		return fmt.Errorf("listing Provider: %w", err)
	}
	log.V(1).Info("setting expectations for Provider", "Provider Count", len(providerList.Items))

	for index := range providerList.Items {
		log.V(1).Info("expecting Provider", "name", providerList.Items[index].GetName())
		t.externalDataProvider.Expect(&providerList.Items[index])
	}
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
	for i := range templates.Items {
		// We don't need to shallow-copy the ConstraintTemplate here. The templates
		// list is used for nothing else, so there is no danger of the object we
		// pass to templates.Expect() changing from underneath us.
		ct := &templates.Items[i]
		log.V(1).Info("expecting template", "name", ct.GetName())
		t.templates.Expect(ct)

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
		t.constraintTrackers.Go(ctx, gvk.String(), func(ctx context.Context) error {
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

	if operations.HasValidationOperations() {
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

	for i := range lst.Items {
		c := &lst.Items[i]
		if c.GetName() != keys.Config.Name || c.GetNamespace() != keys.Config.Namespace {
			log.Info("ignoring unsupported config name", "namespace", c.GetNamespace(), "name", c.GetName())
			continue
		}
		return c, nil
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

// EnableStats enables the verbose logging routine for the readiness tracker.
func (t *Tracker) EnableStats() {
	t.statsEnabled.Set(true)
}

// DisableStats disables the verbose logging routine for the readiness tracker.
func (t *Tracker) DisableStats() {
	t.statsEnabled.Set(false)
}

// statsPrinter handles verbose logging of the readiness tracker outstanding expectations on a regular cadence.
// Runs until the provided context is canceled.
func (t *Tracker) statsPrinter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(statsPeriod):
		}

		if !t.statsEnabled.Get() {
			continue
		}

		if t.Satisfied() {
			return
		}

		if operations.HasValidationOperations() {
			if unsat := t.templates.unsatisfied(); len(unsat) > 0 {
				log.Info("--- begin unsatisfied templates ---", "populated", t.templates.Populated(), "count", len(unsat))
				for _, u := range unsat {
					log.Info("unsatisfied template", "name", u.namespacedName, "gvk", u.gvk)
				}
			}

			for _, gvk := range t.templates.kinds() {
				if t.constraints.Get(gvk).Satisfied() {
					continue
				}
				c := t.constraints.Get(gvk)
				tr, ok := c.(*objectTracker)
				if !ok {
					continue
				}
				unsat := tr.unsatisfied()
				if len(unsat) == 0 {
					continue
				}

				log.Info("--- begin unsatisfied constraints ---", "gvk", gvk, "populated", tr.Populated(), "count", len(unsat))
				for _, u := range unsat {
					log.Info("unsatisfied constraint", "name", u.namespacedName, "gvk", u.gvk)
				}
			}

			for _, gvk := range t.config.kinds() {
				if t.data.Get(gvk).Satisfied() {
					continue
				}
				c := t.data.Get(gvk)
				tr, ok := c.(*objectTracker)
				if !ok {
					continue
				}
				unsat := tr.unsatisfied()
				if len(unsat) == 0 {
					continue
				}

				log.Info("--- Begin unsatisfied data ---", "gvk", gvk, "populated", tr.Populated(), "count", len(unsat))
				for _, u := range unsat {
					log.Info("unsatisfied data", "name", u.namespacedName, "gvk", u.gvk)
				}
			}
		}
		if t.mutationEnabled {
			logUnsatisfiedAssignMetadata(t)
			logUnsatisfiedAssign(t)
			logUnsatisfiedModifySet(t)
		}
		if t.externalDataEnabled {
			logUnsatisfiedExternalDataProvider(t)
		}
	}
}

func logUnsatisfiedAssignMetadata(t *Tracker) {
	for _, amKey := range t.assignMetadata.unsatisfied() {
		log.Info("unsatisfied AssignMetadata", "name", amKey.namespacedName)
	}
}

func logUnsatisfiedAssign(t *Tracker) {
	for _, amKey := range t.assign.unsatisfied() {
		log.Info("unsatisfied Assign", "name", amKey.namespacedName)
	}
}

func logUnsatisfiedModifySet(t *Tracker) {
	for _, amKey := range t.modifySet.unsatisfied() {
		log.Info("unsatisfied ModifySet", "name", amKey.namespacedName)
	}
}

func logUnsatisfiedExternalDataProvider(t *Tracker) {
	for _, amKey := range t.externalDataProvider.unsatisfied() {
		log.Info("unsatisfied Provider", "name", amKey.namespacedName)
	}
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
