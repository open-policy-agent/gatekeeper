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
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"

	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	expansionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1alpha1"
	mutationv1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1alpha1"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var crashOnFailureFetchingExpectations = flag.Bool("crash-on-failure-fetching-expectations", false, "When set (defaults to false), gatekeeper will ignore errors that occur when gathering expectations. This prevents bootstrapping errors from crashing Gatekeeper at the cost of increasing the risk Gatekeeper will under-enforce policy by serving before it has loaded in all policies. Enabling this will help prevent under-enforcement at the risk of crashing during startup for issues like network errors. Note that enabling this flag currently does not achieve the aforementioned effect since fetching expectations are set to retry until success so failures during fetching expectations currently do not occurr.")

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
	syncsets             *objectTracker
	assignMetadata       *objectTracker
	assign               *objectTracker
	modifySet            *objectTracker
	assignImage          *objectTracker
	externalDataProvider *objectTracker
	expansions           *objectTracker
	constraints          *trackerMap
	data                 *trackerMap

	ready                        chan struct{}
	constraintTrackers           *syncutil.SingleRunner
	dataTrackers                 *syncutil.SingleRunner
	statsEnabled                 syncutil.SyncBool
	mutationEnabled              bool
	externalDataEnabled          bool
	expansionEnabled             bool
	crashOnFailure               bool
	trackListerPredicateOverride retryPredicate
}

// NewTracker creates a new Tracker and initializes the internal trackers.
func NewTracker(lister Lister, mutationEnabled, externalDataEnabled, expansionEnabled bool) *Tracker {
	return newTracker(lister, mutationEnabled, externalDataEnabled, expansionEnabled, *crashOnFailureFetchingExpectations, nil, nil)
}

func newTracker(lister Lister, mutationEnabled, externalDataEnabled, expansionEnabled bool, crashOnFailure bool, trackListerPredicateOverride retryPredicate, fn objDataFactory) *Tracker {
	tracker := Tracker{
		lister:             lister,
		templates:          newObjTracker(v1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate"), fn),
		config:             newObjTracker(configv1alpha1.GroupVersion.WithKind("Config"), fn),
		syncsets:           newObjTracker(syncsetv1alpha1.GroupVersion.WithKind("SyncSet"), fn),
		constraints:        newTrackerMap(fn),
		data:               newTrackerMap(fn),
		ready:              make(chan struct{}),
		constraintTrackers: &syncutil.SingleRunner{},
		dataTrackers:       &syncutil.SingleRunner{},

		mutationEnabled:              mutationEnabled,
		externalDataEnabled:          externalDataEnabled,
		expansionEnabled:             expansionEnabled,
		crashOnFailure:               crashOnFailure,
		trackListerPredicateOverride: trackListerPredicateOverride,
	}
	if mutationEnabled {
		tracker.assignMetadata = newObjTracker(mutationv1.GroupVersion.WithKind("AssignMetadata"), fn)
		tracker.assign = newObjTracker(mutationv1.GroupVersion.WithKind("Assign"), fn)
		tracker.modifySet = newObjTracker(mutationv1.GroupVersion.WithKind("ModifySet"), fn)
		tracker.assignImage = newObjTracker(mutationsv1alpha1.GroupVersion.WithKind("AssignImage"), fn)
	}
	if externalDataEnabled {
		tracker.externalDataProvider = newObjTracker(externaldatav1beta1.SchemeGroupVersion.WithKind("Provider"), fn)
	}
	if expansionEnabled {
		tracker.expansions = newObjTracker(expansionv1alpha1.GroupVersion.WithKind("ExpansionTemplate"), fn)
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

	// Do not compare versions. Internally, we index trackers by GroupKind
	switch {
	case gvk.Group == v1beta1.SchemeGroupVersion.Group && gvk.Kind == "ConstraintTemplate":
		if operations.HasValidationOperations() {
			return t.templates
		}
		return noopExpectations{}
	case gvk.Group == syncsetv1alpha1.GroupVersion.Group && gvk.Kind == "SyncSet":
		return t.syncsets
	case gvk.Group == configv1alpha1.GroupVersion.Group && gvk.Kind == "Config":
		return t.config
	case gvk.Group == externaldatav1beta1.SchemeGroupVersion.Group && gvk.Kind == "Provider":
		return t.externalDataProvider
	case gvk.Group == mutationv1.GroupVersion.Group && gvk.Kind == "AssignMetadata":
		if t.mutationEnabled {
			return t.assignMetadata
		}
		return noopExpectations{}
	case gvk.Group == mutationv1.GroupVersion.Group && gvk.Kind == "Assign":
		if t.mutationEnabled {
			return t.assign
		}
		return noopExpectations{}
	case gvk.Group == mutationv1.GroupVersion.Group && gvk.Kind == "ModifySet":
		if t.mutationEnabled {
			return t.modifySet
		}
		return noopExpectations{}
	case gvk.Group == mutationsv1alpha1.GroupVersion.Group && gvk.Kind == "AssignImage":
		if t.mutationEnabled {
			return t.assignImage
		}
		return noopExpectations{}
	case gvk.Group == expansionv1alpha1.GroupVersion.Group && gvk.Kind == "ExpansionTemplate":
		if t.expansionEnabled {
			return t.expansions
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
	if t.config.Populated() && t.syncsets.Populated() && !t.data.Has(gvk) {
		// Return throw-away tracker instead.
		return noopExpectations{}
	}
	return t.data.Get(gvk)
}

// Returns the GVKs for which the Tracker has data expectations.
func (t *Tracker) DataGVKs() []schema.GroupVersionKind {
	return t.data.Keys()
}

func (t *Tracker) templateCleanup(ct *templates.ConstraintTemplate) {
	gvk := constraintGVK(ct)
	t.constraints.Remove(gvk)
	<-t.ready // constraintTrackers are setup in Run()
	t.constraintTrackers.Cancel(gvk.String())
}

// CancelTemplate stops expecting the provided ConstraintTemplate and associated Constraints.
func (t *Tracker) CancelTemplate(ct *templates.ConstraintTemplate) {
	log.V(logging.DebugLevel).Info("cancel tracking for template", "namespace", ct.GetNamespace(), "name", ct.GetName())
	t.templates.CancelExpect(ct)
	t.templateCleanup(ct)
}

// TryCancelTemplate will check the readiness retries left on a CT and
// cancel the expectation for that CT and its associated Constraints if
// no retries remain.
func (t *Tracker) TryCancelTemplate(ct *templates.ConstraintTemplate) {
	log.V(logging.DebugLevel).Info("try to cancel tracking for template", "namespace", ct.GetNamespace(), "name", ct.GetName())
	if t.templates.TryCancelExpect(ct) {
		t.templateCleanup(ct)
	}
}

// CancelData stops expecting data for the specified resource kind.
func (t *Tracker) CancelData(gvk schema.GroupVersionKind) {
	log.V(logging.DebugLevel).Info("cancel tracking for data", "gvk", gvk)
	t.data.Remove(gvk)
	<-t.ready
	t.dataTrackers.Cancel(gvk.String())
}

func (t *Tracker) TryCancelData(gvk schema.GroupVersionKind) {
	log.V(logging.DebugLevel).Info("try to cancel tracking for data", "gvk", gvk)
	if t.data.TryCancel(gvk) {
		<-t.ready
		t.dataTrackers.Cancel(gvk.String())
	}
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
		if !t.assignMetadata.Satisfied() || !t.assign.Satisfied() || !t.modifySet.Satisfied() || !t.assignImage.Satisfied() {
			return false
		}
		log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "assignMetadata")
		log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "assign")
		log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "modifySet")
		log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "assignImage")
	}

	if t.externalDataEnabled {
		if !t.externalDataProvider.Satisfied() {
			return false
		}
		log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "provider")
	}

	if t.expansionEnabled {
		if !t.expansions.Satisfied() {
			return false
		}
		log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "expansiontemplates")
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
	log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "constraints")

	if !t.config.Satisfied() {
		log.V(logging.DebugLevel).Info("expectations unsatisfied", "tracker", "config")
		return false
	}

	if operations.HasValidationOperations() {
		if !t.syncsets.Satisfied() {
			log.V(logging.DebugLevel).Info("expectations unsatisfied", "tracker", "syncset")
			return false
		}

		for _, gvk := range t.DataGVKs() {
			if !t.data.Get(gvk).Satisfied() {
				log.V(logging.DebugLevel).Info("expectations unsatisfied", "tracker", "data", "gvk", gvk)
				return false
			}
		}
	}
	log.V(logging.DebugLevel).Info("all expectations satisfied", "tracker", "data")

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
	var grp *errgroup.Group
	grp = &errgroup.Group{}
	gctx := ctx
	if t.crashOnFailure {
		grp, gctx = errgroup.WithContext(ctx)
	}
	t.constraintTrackers = syncutil.RunnerWithContext(gctx)
	t.dataTrackers = syncutil.RunnerWithContext(gctx)
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
		grp.Go(func() error {
			return t.trackAssignImage(gctx)
		})
	}
	if t.externalDataEnabled {
		grp.Go(func() error {
			return t.trackExternalDataProvider(gctx)
		})
	}
	if t.expansionEnabled {
		grp.Go(func() error {
			return t.trackExpansionTemplates(gctx)
		})
	}
	if operations.HasValidationOperations() {
		grp.Go(func() error {
			return t.trackConstraintTemplates(gctx)
		})
	}
	grp.Go(func() error {
		return t.trackConfigAndSyncSets(gctx)
	})

	go t.statsPrinter(ctx)

	// start deleted object polling. Periodically collects
	// objects that are expected by the Tracker, but are deleted
	go func() error {
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
	}()

	if err := grp.Wait(); err != nil && t.crashOnFailure {
		return err
	}

	// constraintTrackers & dataTrackers Wait() must appear after grp.Wait() - allows trackConstraintTemplates() & trackConfigAndSyncSets() time to schedule their sub-tasks.
	if err := t.constraintTrackers.Wait(); err != nil && t.crashOnFailure {
		return err
	}

	if err := t.dataTrackers.Wait(); err != nil && t.crashOnFailure {
		return err
	}

	return nil
}

func (t *Tracker) Populated() bool {
	mutationPopulated := true
	if t.mutationEnabled {
		// If !t.mutationEnabled and we call this, it yields a null pointer exception
		mutationPopulated = t.assignMetadata.Populated() && t.assign.Populated() && t.modifySet.Populated() && t.assignImage.Populated()
	}
	externalDataProviderPopulated := true
	if t.externalDataEnabled {
		// If !t.externalDataEnabled and we call this, it yields a null pointer exception
		externalDataProviderPopulated = t.externalDataProvider.Populated()
	}
	validationPopulated := true
	if operations.HasValidationOperations() {
		validationPopulated = t.templates.Populated() && t.constraints.Populated() && t.data.Populated() && t.syncsets.Populated()
	}
	return validationPopulated && t.config.Populated() && mutationPopulated && externalDataProviderPopulated
}

// Returns whether both the Config and all SyncSet expectations have been Satisfied.
func (t *Tracker) SyncSetAndConfigSatisfied() bool {
	satisfied := t.config.Satisfied()
	if operations.HasValidationOperations() {
		satisfied = satisfied && t.syncsets.Satisfied()
	}

	return satisfied
}

// collectForObjectTracker identifies objects that are unsatisfied for the provided
// `es`, which must be an objectTracker, and removes those expectations.
func (t *Tracker) collectForObjectTracker(ctx context.Context, es Expectations, cleanup func(schema.GroupVersionKind), trackerName string) error {
	if es == nil {
		return fmt.Errorf("nil Expectations provided to collectForObjectTracker")
	}

	if !es.Populated() {
		log.V(logging.DebugLevel).Info("Expectations unpopulated, skipping collection", "tracker", trackerName)
		return nil
	}

	if es.Satisfied() {
		log.V(logging.DebugLevel).Info("Expectations already satisfied, skipping collection", "tracker", trackerName)
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

		log.V(logging.DebugLevel).Info("canceling expectations", "name", u.GetName(), "namespace", u.GetNamespace(), "gvk", u.GroupVersionKind(), "tracker", trackerName)
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
	err := t.collectForObjectTracker(ctx, tt, cleanupTemplate, "ConstraintTemplate")
	if err != nil {
		log.Error(err, "while collecting for the ConstraintTemplate tracker")
	}

	ct := t.config
	err = t.collectForObjectTracker(ctx, ct, nil, "Config")
	if err != nil {
		log.Error(err, "while collecting for the Config tracker")
	}

	err = t.collectForObjectTracker(ctx, t.syncsets, nil, "SyncSet")
	if err != nil {
		log.Error(err, "while collecting for the SyncSet tracker")
	}

	// collect deleted but expected constraints
	for _, gvk := range t.constraints.Keys() {
		// retrieve the expectations for this key
		es := t.constraints.Get(gvk)
		// the GVK of a constraint has the term "constraint" in it already
		err = t.collectForObjectTracker(ctx, es, nil, gvk.String())
		if err != nil {
			log.Error(err, "while collecting for the Constraint type", "gvk", gvk)
			continue
		}
	}

	// collect data expects
	for _, gvk := range t.DataGVKs() {
		// retrieve the expectations for this key
		es := t.data.Get(gvk)
		err = t.collectForObjectTracker(ctx, es, nil, fmt.Sprintf("%s/ %s", "Data", gvk))
		if err != nil {
			log.Error(err, "while collecting for the Data type", "gvk", gvk)
			continue
		}
	}
}

func (t *Tracker) trackAssignMetadata(ctx context.Context) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			t.assignMetadata.ExpectationsDone()
			log.V(logging.DebugLevel).Info("AssignMetadata expectations populated")
		}
	}()

	if !t.mutationEnabled {
		return nil
	}

	assignMetadataList := &mutationv1.AssignMetadataList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, assignMetadataList); err != nil {
		hadError = true
		log.Error(err, "listing AssignMetadata")
		return fmt.Errorf("listing AssignMetadata: %w", err)
	}
	log.V(logging.DebugLevel).Info("setting expectations for AssignMetadata", "AssignMetadata Count", len(assignMetadataList.Items))

	for index := range assignMetadataList.Items {
		log.V(logging.DebugLevel).Info("expecting AssignMetadata", "name", assignMetadataList.Items[index].GetName())
		t.assignMetadata.Expect(&assignMetadataList.Items[index])
	}
	return nil
}

func (t *Tracker) trackAssign(ctx context.Context) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			t.assign.ExpectationsDone()
			log.V(logging.DebugLevel).Info("Assign expectations populated")
		}
	}()

	if !t.mutationEnabled {
		return nil
	}

	assignList := &mutationv1.AssignList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, assignList); err != nil {
		hadError = true
		log.Error(err, "listing Assign")
		return fmt.Errorf("listing Assign: %w", err)
	}
	log.V(logging.DebugLevel).Info("setting expectations for Assign", "Assign Count", len(assignList.Items))

	for index := range assignList.Items {
		log.V(logging.DebugLevel).Info("expecting Assign", "name", assignList.Items[index].GetName())
		t.assign.Expect(&assignList.Items[index])
	}
	return nil
}

func (t *Tracker) trackModifySet(ctx context.Context) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			t.modifySet.ExpectationsDone()
			log.V(logging.DebugLevel).Info("ModifySet expectations populated")
		}
	}()

	if !t.mutationEnabled {
		return nil
	}

	modifySetList := &mutationv1.ModifySetList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, modifySetList); err != nil {
		hadError = true
		log.Error(err, "listing ModifySet")
		return fmt.Errorf("listing ModifySet: %w", err)
	}
	log.V(logging.DebugLevel).Info("setting expectations for ModifySet", "ModifySet Count", len(modifySetList.Items))

	for index := range modifySetList.Items {
		log.V(logging.DebugLevel).Info("expecting ModifySet", "name", modifySetList.Items[index].GetName())
		t.modifySet.Expect(&modifySetList.Items[index])
	}
	return nil
}

func (t *Tracker) trackAssignImage(ctx context.Context) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			t.assignImage.ExpectationsDone()
			log.V(logging.DebugLevel).Info("AssignImage expectations populated")
		}
	}()

	if !t.mutationEnabled {
		return nil
	}

	assignImageList := &mutationsv1alpha1.AssignImageList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, assignImageList); err != nil {
		hadError = true
		log.Error(err, "listing AssignImage")
		return fmt.Errorf("listing AssignImage: %w", err)
	}
	log.V(logging.DebugLevel).Info("setting expectations for AssignImage", "AssignImage Count", len(assignImageList.Items))

	for index := range assignImageList.Items {
		log.V(logging.DebugLevel).Info("expecting AssignImage", "name", assignImageList.Items[index].GetName())
		t.assignImage.Expect(&assignImageList.Items[index])
	}
	return nil
}

func (t *Tracker) trackExpansionTemplates(ctx context.Context) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			t.expansions.ExpectationsDone()
			log.V(logging.DebugLevel).Info("ExpansionTemplate expectations populated")
		}
	}()

	if !t.expansionEnabled {
		return nil
	}

	expansionList := &expansionv1alpha1.ExpansionTemplateList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, expansionList); err != nil {
		hadError = true
		log.Error(err, "listing ExpansionTemplate")
		return fmt.Errorf("listing ExpansionTemplate: %w", err)
	}
	log.V(logging.DebugLevel).Info("setting expectations for ExpansionTemplate", "ExpansionTemplate Count", len(expansionList.Items))

	for index := range expansionList.Items {
		log.V(logging.DebugLevel).Info("expecting ExpansionTemplate", "name", expansionList.Items[index].GetName())
		t.expansions.Expect(&expansionList.Items[index])
	}
	return nil
}

func (t *Tracker) trackExternalDataProvider(ctx context.Context) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			t.externalDataProvider.ExpectationsDone()
			log.V(logging.DebugLevel).Info("Provider expectations populated")
		}
	}()

	if !t.externalDataEnabled {
		return nil
	}

	providerList := &externaldatav1beta1.ProviderList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, providerList); err != nil {
		hadError = true
		log.Error(err, "listing Provider")
		return fmt.Errorf("listing Provider: %w", err)
	}
	log.V(logging.DebugLevel).Info("setting expectations for Provider", "Provider Count", len(providerList.Items))

	for index := range providerList.Items {
		log.V(logging.DebugLevel).Info("expecting Provider", "name", providerList.Items[index].GetName())
		t.externalDataProvider.Expect(&providerList.Items[index])
	}
	return nil
}

func (t *Tracker) trackConstraintTemplates(ctx context.Context) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			t.templates.ExpectationsDone()
			log.V(logging.DebugLevel).Info("template expectations populated")
		}
	}()

	templates := &v1beta1.ConstraintTemplateList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, templates); err != nil {
		hadError = true
		log.Error(err, "listing templates")
		return fmt.Errorf("listing templates: %w", err)
	}

	log.V(logging.DebugLevel).Info("setting expectations for templates", "templateCount", len(templates.Items))

	handled := make(map[schema.GroupVersionKind]bool, len(templates.Items))
	for i := range templates.Items {
		// We don't need to shallow-copy the ConstraintTemplate here. The templates
		// list is used for nothing else, so there is no danger of the object we
		// pass to templates.Expect() changing from underneath us.
		ct := &templates.Items[i]
		log.V(logging.DebugLevel).Info("expecting template", "name", ct.GetName())
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
				if t.crashOnFailure {
					return err
				}
			}
			return nil
		})
	}
	return nil
}

// trackConfigAndSyncSets sets expectations for cached data as specified by the singleton Config resource.
// and any SyncSet resources present on the cluster.
// Works best effort and fails-open if a resource cannot be fetched or does not exist.
func (t *Tracker) trackConfigAndSyncSets(ctx context.Context) error {
	var retErr error
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || retErr == nil {
			t.config.ExpectationsDone()
			log.V(logging.DebugLevel).Info("config expectations populated")

			t.syncsets.ExpectationsDone()
			log.V(logging.DebugLevel).Info("syncset expectations populated")
		}
	}()

	dataGVKs := make(map[schema.GroupVersionKind]struct{})
	cfg, err := t.getConfigResource(ctx)
	if err != nil {
		log.Error(err, "fetching config resources")
		retErr = fmt.Errorf("fetching config resources: %w", err)
		if t.crashOnFailure {
			return retErr
		}
	} else {
		if cfg == nil {
			log.Info("config resource not found - skipping for readiness")
		} else {
			if !cfg.GetDeletionTimestamp().IsZero() {
				log.Info("config resource is being deleted - skipping for readiness")
			} else {
				t.config.Expect(cfg)
				log.V(logging.DebugLevel).Info("setting expectations for config", "configCount", 1)

				for _, entry := range cfg.Spec.Sync.SyncOnly {
					dataGVKs[entry.ToGroupVersionKind()] = struct{}{}
				}
			}
		}
	}

	// Without validation operations, there is no reason to wait for referential data when deciding readiness.
	if !operations.HasValidationOperations() {
		return retErr
	}

	syncsets := &syncsetv1alpha1.SyncSetList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, syncsets); err != nil {
		log.Error(err, "fetching syncset resources")
		retErr = fmt.Errorf("fetching syncset resources: %w", err)
		if t.crashOnFailure {
			return retErr
		}
	} else {
		log.V(logging.DebugLevel).Info("setting expectations for syncsets", "syncsetCount", len(syncsets.Items))
		for i := range syncsets.Items {
			syncset := syncsets.Items[i]

			t.syncsets.Expect(&syncset)
			log.V(logging.DebugLevel).Info("expecting syncset", "name", syncset.GetName(), "namespace", syncset.GetNamespace())

			for i := range syncset.Spec.GVKs {
				gvk := syncset.Spec.GVKs[i].ToGroupVersionKind()
				if _, ok := dataGVKs[gvk]; ok {
					log.Info("duplicate GVK to sync", "gvk", gvk)
				}

				dataGVKs[gvk] = struct{}{}
			}
		}
	}

	// Expect the resource kinds specified in the Config resource and all SyncSet resources.
	// We will fail-open (resolve expectations) for GVKs that are unregistered.
	for gvk := range dataGVKs {
		gvkCpy := gvk

		// Set expectations for individual cached resources
		dt := t.ForData(gvkCpy)
		t.dataTrackers.Go(ctx, gvk.String(), func(ctx context.Context) error {
			err := t.trackData(ctx, gvkCpy, dt)
			if err != nil {
				log.Error(err, "aborted trackData", "gvk", gvkCpy)
				if t.crashOnFailure {
					return err
				}
			}
			return nil
		})
	}

	return retErr
}

// getConfigResource returns the Config singleton if present.
// Returns a nil reference if it is not found.
func (t *Tracker) getConfigResource(ctx context.Context) (*configv1alpha1.Config, error) {
	lst := &configv1alpha1.ConfigList{}
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, lst); err != nil {
		log.Error(err, "listing config")
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
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			dt.ExpectationsDone()
			log.V(logging.DebugLevel).Info("data expectations populated", "gvk", gvk)
		}
	}()

	// List individual resources and expect observations of each in the sync controller.
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})
	// NoKindMatchError is non-recoverable, otherwise we'll retry.
	retryPred := retryUnlessUnregistered
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	err := lister.List(ctx, u)
	if err != nil {
		hadError = true
		log.Error(err, "listing data", "gvk", gvk)
		return fmt.Errorf("listing data: %w", err)
	}

	for i := range u.Items {
		item := &u.Items[i]
		dt.Expect(item)
		log.V(logging.DebugLevel).Info("expecting data", "gvk", item.GroupVersionKind(), "namespace", item.GetNamespace(), "name", item.GetName())
	}
	return nil
}

// trackConstraints sets expectations for all constraints managed by a template.
// Blocks until constraints can be listed or context is canceled.
func (t *Tracker) trackConstraints(ctx context.Context, gvk schema.GroupVersionKind, constraints Expectations) error {
	hadError := false
	defer func() {
		// If we are ignoring errors when tracking expecations, we need to set expectations to done to prevent readiness tracker never being satisfied
		if !t.crashOnFailure || !hadError {
			constraints.ExpectationsDone()
			log.V(logging.DebugLevel).Info("constraint expectations populated", "gvk", gvk)
		}
	}()

	u := unstructured.UnstructuredList{}
	u.SetGroupVersionKind(gvk)
	retryPred := retryAll
	if t.trackListerPredicateOverride != nil {
		retryPred = t.trackListerPredicateOverride
	}
	lister := retryLister(t.lister, retryPred)
	if err := lister.List(ctx, &u); err != nil {
		hadError = true
		log.Error(err, "listing constraints")
		return fmt.Errorf("listing constraints: %w", err)
	}

	for i := range u.Items {
		o := u.Items[i]
		constraints.Expect(&o)
		log.V(logging.DebugLevel).Info("expecting Constraint", "gvk", gvk, "name", objectName(&o))
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

			for _, gvk := range t.DataGVKs() {
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

			logUnsatisfiedSyncSet(t)
			logUnsatisfiedConfig(t)
		}
		if t.mutationEnabled {
			logUnsatisfiedAssignMetadata(t)
			logUnsatisfiedAssign(t)
			logUnsatisfiedModifySet(t)
			logUnsatisfiedAssignImage(t)
		}
		if t.externalDataEnabled {
			logUnsatisfiedExternalDataProvider(t)
		}
		if t.expansionEnabled {
			logUnsatisfiedExpansions(t)
		}
	}
}

func logUnsatisfiedSyncSet(t *Tracker) {
	if unsat := t.syncsets.unsatisfied(); len(unsat) > 0 {
		log.Info("--- Begin unsatisfied syncsets ---", "populated", t.syncsets.Populated(), "count", len(unsat))

		for _, k := range unsat {
			log.Info("unsatisfied SyncSet", "name", k.namespacedName)
		}
		log.Info("--- End unsatisfied syncsets ---")
	}
}

func logUnsatisfiedConfig(t *Tracker) {
	if !t.config.Satisfied() {
		log.Info("--- Begin unsatisfied config ---", "populated", t.config.Populated(), "count", 1)
		log.Info("unsatisfied Config", "name", keys.Config)
		log.Info("--- End unsatisfied config ---")
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

func logUnsatisfiedAssignImage(t *Tracker) {
	for _, amKey := range t.assignImage.unsatisfied() {
		log.Info("unsatisfied AssignImage", "name", amKey.namespacedName)
	}
}

func logUnsatisfiedExpansions(t *Tracker) {
	for _, et := range t.expansions.unsatisfied() {
		log.Info("unsatisfied ExpansionTemplate", "name", et.namespacedName)
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
