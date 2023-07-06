package cachemanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("cache-manager")

type CacheManagerConfig struct {
	Opa              syncutil.OpaDataClient
	SyncMetricsCache *syncutil.MetricsCache
	Tracker          *readiness.Tracker
	ProcessExcluder  *process.Excluder
	Registrar        *watch.Registrar
	WatchedSet       *watch.Set
	GVKAggregator    *aggregator.GVKAgreggator
	Reader           client.Reader
}

type CacheManager struct {
	// the processExcluder and the gvkAggregator define what the underlying
	// cache should look like. we refer to those two as "the spec"
	processExcluder *process.Excluder
	gvkAggregator   *aggregator.GVKAgreggator
	// mu guards access to any part of the spec above
	mu sync.RWMutex

	opa              syncutil.OpaDataClient
	syncMetricsCache *syncutil.MetricsCache
	tracker          *readiness.Tracker
	registrar        *watch.Registrar
	watchedSet       *watch.Set
	replayErrChan    chan error
	replayTicker     time.Ticker
	reader           client.Reader
	excluderChanged  bool
}

func NewCacheManager(config *CacheManagerConfig) (*CacheManager, error) {
	if config.WatchedSet == nil {
		return nil, fmt.Errorf("watchedSet must be non-nil")
	}
	if config.Registrar == nil {
		return nil, fmt.Errorf("registrar must be non-nil")
	}
	if config.ProcessExcluder == nil {
		return nil, fmt.Errorf("processExcluder must be non-nil")
	}
	if config.Tracker == nil {
		return nil, fmt.Errorf("tracker must be non-nil")
	}
	if config.Reader == nil {
		return nil, fmt.Errorf("reader must be non-nil")
	}

	cm := &CacheManager{
		opa:              config.Opa,
		syncMetricsCache: config.SyncMetricsCache,
		tracker:          config.Tracker,
		processExcluder:  config.ProcessExcluder,
		registrar:        config.Registrar,
		watchedSet:       config.WatchedSet,
		reader:           config.Reader,
	}

	cm.gvkAggregator = aggregator.NewGVKAggregator()

	cm.replayTicker = *time.NewTicker(3 * time.Second)

	return cm, nil
}

func (c *CacheManager) Start(ctx context.Context) error {
	go c.updateDatastore(ctx)

	<-ctx.Done()
	return nil
}

// AddSource adjusts the watched set of gvks according to the newGVKs passed in
// for a given sourceKey.
func (c *CacheManager) AddSource(ctx context.Context, sourceKey aggregator.Key, newGVKs []schema.GroupVersionKind) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.gvkAggregator.Upsert(sourceKey, newGVKs); err != nil {
		return fmt.Errorf("internal error adding source: %w", err)
	}

	return nil
}

func (c *CacheManager) RemoveSource(ctx context.Context, sourceKey aggregator.Key) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.gvkAggregator.Remove(sourceKey); err != nil {
		return fmt.Errorf("internal error removing source: %w", err)
	}

	return nil
}

func (c *CacheManager) ExcludeProcesses(newExcluder *process.Excluder) {
	if c.processExcluder.Equals(newExcluder) {
		return
	}

	c.mu.Lock()
	c.processExcluder.Replace(newExcluder)
	// there is a new excluder which means we need to schedule a wipe for any
	// previously watched GVKs to be re-added to get a chance to be evaluated
	// for this new process excluder.
	c.excluderChanged = true
	c.mu.Unlock()
}

func (c *CacheManager) AddObject(ctx context.Context, instance *unstructured.Unstructured) error {
	isNamespaceExcluded, err := c.processExcluder.IsNamespaceExcluded(process.Sync, instance)
	if err != nil {
		return fmt.Errorf("error while excluding namespaces: %w", err)
	}

	// bail because it means we should not be
	// syncing this gvk
	if isNamespaceExcluded {
		c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)
		return nil
	}

	syncKey := syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName())
	_, err = c.opa.AddData(ctx, instance)
	if err != nil {
		c.syncMetricsCache.AddObject(
			syncKey,
			syncutil.Tags{
				Kind:   instance.GetKind(),
				Status: metrics.ErrorStatus,
			},
		)

		return err
	}

	c.tracker.ForData(instance.GroupVersionKind()).Observe(instance)

	c.syncMetricsCache.AddObject(syncKey, syncutil.Tags{
		Kind:   instance.GetKind(),
		Status: metrics.ActiveStatus,
	})
	c.syncMetricsCache.AddKind(instance.GetKind())

	return err
}

func (c *CacheManager) RemoveObject(ctx context.Context, instance *unstructured.Unstructured) error {
	if _, err := c.opa.RemoveData(ctx, instance); err != nil {
		return err
	}

	// only delete from metrics map if the data removal was succcesful
	c.syncMetricsCache.DeleteObject(syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName()))
	c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)

	return nil
}

func (c *CacheManager) wipeData(ctx context.Context) error {
	if _, err := c.opa.RemoveData(ctx, target.WipeData()); err != nil {
		return err
	}

	// reset sync cache before sending the metric
	c.syncMetricsCache.ResetCache()
	c.syncMetricsCache.ReportSync()

	return nil
}

func (c *CacheManager) ReportSyncMetrics() {
	c.syncMetricsCache.ReportSync()
}

func (c *CacheManager) listAndSyncDataForGVK(ctx context.Context, gvk schema.GroupVersionKind) error {
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	err := c.reader.List(ctx, u)
	if err != nil {
		return fmt.Errorf("replaying data for %+v: %w", gvk, err)
	}

	defer c.ReportSyncMetrics()

	for i := range u.Items {
		if err := c.AddObject(ctx, &u.Items[i]); err != nil {
			return fmt.Errorf("adding data for %+v: %w", gvk, err)
		}
	}

	return nil
}

func (c *CacheManager) updateDatastore(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.replayTicker.C:
			// snapshot the current spec so we can make a step upgrade
			// to the contests of the opa cache.
			c.mu.RLock()
			currentGVKsInAgg := watch.NewSet()
			currentGVKsInAgg.Add(c.gvkAggregator.ListAllGVKs()...)
			excluderChanged := c.excluderChanged
			c.mu.RUnlock()

			c.makeUpdatesForSpecInTime(ctx, currentGVKsInAgg, excluderChanged)
		}
	}
}

// listAndSyncData returns a set of gvks that were successfully listed and synced.
func (c *CacheManager) listAndSyncData(ctx context.Context, gvks []schema.GroupVersionKind, reader client.Reader) *watch.Set {
	gvksSuccessfullySynced := watch.NewSet()
	for _, gvk := range gvks {
		err := c.listAndSyncDataForGVK(ctx, gvk)
		if err != nil {
			log.Error(err, "internal: error syncing gvks cache data")
			// we don't remove this gvk as we will try to re-add it later
			// we also don't return on this error to be able to list and sync
			// other gvks in order to protect against a bad gvk.
		} else {
			gvksSuccessfullySynced.Add(gvk)
		}
	}
	return gvksSuccessfullySynced
}

// makeUpdatesForSpecInTime performs a conditional wipe followed by a replay if necessary as
// given by the current spec (currentGVKsInAgg, excluderChanged) at the time of the call.
func (c *CacheManager) makeUpdatesForSpecInTime(ctx context.Context, currentGVKsInAgg *watch.Set, excluderChanged bool) {
	if c.watchedSet.Equals(currentGVKsInAgg) && !excluderChanged {
		return // nothing to do if both sets are the same and the excluder didn't change
	}

	// replace the current watch set for the sync_controller to pick up
	// any updates on said GVKs.
	// also save the current watch set to make cache changes later
	oldWatchSet := watch.NewSet()
	oldWatchSet.AddSet(c.watchedSet)

	var innerError error
	c.watchedSet.Replace(currentGVKsInAgg, func() {
		// *Note the following steps are not transactional with respect to admission control*

		// Important: dynamic watches update must happen *after* updating our watchSet.
		// Otherwise, the sync controller will drop events for the newly watched kinds.
		// Defer error handling so object re-sync happens even if the watch is hard
		// errored due to a missing GVK in the watch set.
		innerError = c.registrar.ReplaceWatch(ctx, currentGVKsInAgg.Items())
	})
	if innerError != nil {
		log.Error(innerError, "internal: error replacing watch set")
	}

	gvksToDelete := oldWatchSet.Difference(currentGVKsInAgg).Items()
	newGVKsToSync := currentGVKsInAgg.Difference(oldWatchSet)

	// remove any gvks not needing to be synced anymore
	// or re evaluate all if the excluder changed.
	if len(gvksToDelete) > 0 || excluderChanged {
		if err := c.wipeData(ctx); err != nil {
			log.Error(err, "internal: error wiping cache")
		}

		if excluderChanged {
			c.unsetExcluderChanged()
		}

		// everything that gets wiped needs to be readded
		newGVKsToSync.AddSet(currentGVKsInAgg)
	}

	// sync net new gvks and potentially replayed gvks from the cache wipe above
	gvksSynced := c.listAndSyncData(ctx, newGVKsToSync.Items(), c.reader)

	gvksNotSynced := gvksSynced.Difference(newGVKsToSync)
	for _, gvk := range gvksNotSynced.Items() {
		log.Info(fmt.Sprintf("failed to sync gvk: %s; will retry", gvk))
	}
}

func (c *CacheManager) unsetExcluderChanged() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// unset the excluderChanged bool now
	c.excluderChanged = false
}
