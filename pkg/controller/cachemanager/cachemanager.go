package cachemanager

import (
	"context"
	"fmt"
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
	opa              syncutil.OpaDataClient
	syncMetricsCache *syncutil.MetricsCache
	tracker          *readiness.Tracker
	processExcluder  *process.Excluder
	registrar        *watch.Registrar
	watchedSet       *watch.Set
	gvkAggregator    *aggregator.GVKAgreggator
	gvksToRemove     *watch.Set
	gvksToSync       *watch.Set
	replayErrChan    chan error
	replayTicker     time.Ticker
	reader           client.Reader
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
	cm.gvksToRemove = watch.NewSet()
	cm.gvksToSync = watch.NewSet()

	cm.replayTicker = *time.NewTicker(3 * time.Second)

	return cm, nil
}

func (c *CacheManager) Start(ctx context.Context) error {
	go c.updateDatastore(ctx)

	<-ctx.Done()
	return nil
}

// WatchGVKsToSync adjusts the watched set of gvks according to the newGVKs passed in
// for a given {syncSourceType, syncSourceName}.
func (c *CacheManager) WatchGVKsToSync(ctx context.Context, newGVKs []schema.GroupVersionKind, newExcluder *process.Excluder, syncSourceType, syncSourceName string) error {
	netNewGVKs := []schema.GroupVersionKind{}
	for _, gvk := range newGVKs {
		if !c.gvkAggregator.IsPresent(gvk) {
			netNewGVKs = append(netNewGVKs, gvk)
		}
	}
	// mark these gvks for the background goroutine to sync
	c.gvksToSync.Add(netNewGVKs...)

	opKey := aggregator.Key{Source: syncSourceType, ID: syncSourceName}
	currentGVKsForKey := c.gvkAggregator.List(opKey)

	if len(newGVKs) == 0 {
		// we are not syncing anything for this key anymore
		if err := c.gvkAggregator.Remove(opKey); err != nil {
			return fmt.Errorf("internal error removing gvks for aggregation: %w", err)
		}
	} else {
		if err := c.gvkAggregator.Upsert(opKey, newGVKs); err != nil {
			return fmt.Errorf("internal error upserting gvks for aggregation: %w", err)
		}
	}

	// stage the new watch set for events for the sync_controller to be
	// the current watch set ... [1/3]
	newGvkWatchSet := watch.NewSet()
	newGvkWatchSet.AddSet(c.watchedSet)
	// ... plus the net new gvks we are adding ... [2/3]
	newGvkWatchSet.Add(netNewGVKs...)

	gvksToDeleteCandidates := getGVKsToDeleteCandidates(newGVKs, currentGVKsForKey)
	gvksToDeleteSet := watch.NewSet()
	for _, gvk := range gvksToDeleteCandidates {
		// if this gvk is no longer required by any source, schedule it to be deleted
		if !c.gvkAggregator.IsPresent(gvk) {
			// Remove expectations for resources we no longer watch.
			c.tracker.CancelData(gvk)
			// mark these gvks for the background goroutine to un-sync
			gvksToDeleteSet.Add(gvk)
		}
	}
	c.gvksToRemove.AddSet(gvksToDeleteSet)

	// ... less the gvks to delete. [3/3]
	newGvkWatchSet.RemoveSet(gvksToDeleteSet)

	// If the watch set has not changed AND the process excluder is the same we're done here.
	if c.watchedSet.Equals(newGvkWatchSet) && newExcluder != nil {
		if c.processExcluder.Equals(newExcluder) {
			return nil
		} else {
			// there is a new excluder which means we need to schedule a wipe for any
			// previously watched GVKs to be re-added to get a chance to be evaluated
			// for this new process excluder.

			c.gvksToRemove.AddSet(newGvkWatchSet)
		}
	}

	// Start watching the newly added gvks set
	var innerError error
	c.watchedSet.Replace(newGvkWatchSet, func() {
		// swapping with the new excluder
		if newExcluder != nil {
			c.processExcluder.Replace(newExcluder)
		}

		// *Note the following steps are not transactional with respect to admission control*

		// Important: dynamic watches update must happen *after* updating our watchSet.
		// Otherwise, the sync controller will drop events for the newly watched kinds.
		// Defer error handling so object re-sync happens even if the watch is hard
		// errored due to a missing GVK in the watch set.
		innerError = c.registrar.ReplaceWatch(ctx, newGvkWatchSet.Items())
	})
	if innerError != nil {
		return innerError
	}

	return nil
}

// returns GVKs that are in currentGVKsForKey but not in newGVKs.
func getGVKsToDeleteCandidates(newGVKs []schema.GroupVersionKind, currentGVKsForKey map[schema.GroupVersionKind]struct{}) []schema.GroupVersionKind {
	newGVKSet := make(map[schema.GroupVersionKind]struct{})
	for _, gvk := range newGVKs {
		newGVKSet[gvk] = struct{}{}
	}

	var toDelete []schema.GroupVersionKind
	for gvk := range currentGVKsForKey {
		if _, found := newGVKSet[gvk]; !found {
			toDelete = append(toDelete, gvk)
		}
	}

	return toDelete
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

func (c *CacheManager) WipeData(ctx context.Context) error {
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

func (c *CacheManager) listAndSyncDataForGVK(ctx context.Context, gvk schema.GroupVersionKind, reader client.Reader) error {
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	err := reader.List(ctx, u)
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
			c.makeUpdates(ctx)
		}
	}
}

// listAndSyncData returns a set of gvks that were successfully listed and synced.
func (c *CacheManager) listAndSyncData(ctx context.Context, gvks []schema.GroupVersionKind, reader client.Reader) *watch.Set {
	gvksSuccessfullySynced := watch.NewSet()
	for _, gvk := range gvks {
		err := c.listAndSyncDataForGVK(ctx, gvk, c.reader)
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

// makeUpdates performs a conditional wipe followed by a replay if necessary.
func (c *CacheManager) makeUpdates(ctx context.Context) {
	// first, wipe the cache if needed
	gvksToDelete := c.gvksToRemove.Items()
	if len(gvksToDelete) > 0 {
		// "checkpoint save" what needs to be replayed
		gvksToReplay := c.gvkAggregator.ListAllGVKs()
		// and add it to be synced below
		c.gvksToSync.Add(gvksToReplay...)

		if err := c.WipeData(ctx); err != nil {
			log.Error(err, "internal: error wiping cache")
			// don't alter the toRemove set, we will try again
		} else {
			c.gvksToRemove.Remove(gvksToDelete...)
			// any gvks that were just removed shouldn't be synced
			c.gvksToSync.Remove(gvksToDelete...)
		}
	}

	// sync net new gvks
	gvksToSyncList := c.gvksToSync.Items()
	gvksSynced := c.listAndSyncData(ctx, gvksToSyncList, c.reader)
	c.gvksToSync.RemoveSet(gvksSynced)
}
