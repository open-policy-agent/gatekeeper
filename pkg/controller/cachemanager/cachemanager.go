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
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("cache-manager")

type Config struct {
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
	processExcluder *process.Excluder
	gvkAggregator   *aggregator.GVKAgreggator
	gvksToRelist    *watch.Set
	excluderChanged bool
	// mu guards access to any of the fields above
	mu sync.RWMutex

	opa                   syncutil.OpaDataClient
	syncMetricsCache      *syncutil.MetricsCache
	tracker               *readiness.Tracker
	registrar             *watch.Registrar
	watchedSet            *watch.Set
	cacheManagementTicker time.Ticker
	reader                client.Reader
}

func NewCacheManager(config *Config) (*CacheManager, error) {
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
	cm.gvksToRelist = watch.NewSet()
	cm.cacheManagementTicker = *time.NewTicker(3 * time.Second)

	return cm, nil
}

func (c *CacheManager) Start(ctx context.Context) error {
	go c.manageCache(ctx)

	<-ctx.Done()
	return nil
}

// AddSource adjusts the watched set of gvks according to the newGVKs passed in
// for a given sourceKey.
func (c *CacheManager) AddSource(ctx context.Context, sourceKey aggregator.Key, newGVKs []schema.GroupVersionKind) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// for this source, find the net new gvks;
	// we will establish new watches for them.
	netNewGVKs := []schema.GroupVersionKind{}
	for _, gvk := range newGVKs {
		if !c.gvkAggregator.IsPresent(gvk) {
			netNewGVKs = append(netNewGVKs, gvk)
		}
	}

	if err := c.gvkAggregator.Upsert(sourceKey, newGVKs); err != nil {
		return fmt.Errorf("internal error adding source: %w", err)
	}
	// as a result of upserting the new gvks for the source key, some gvks
	// may become unreferenced and need to be deleted; this will be handled async
	// in the manageCache loop.

	newGvkWatchSet := watch.NewSet()
	newGvkWatchSet.AddSet(c.watchedSet)
	newGvkWatchSet.Add(netNewGVKs...)

	if newGvkWatchSet.Size() != 0 {
		// watch the net new gvks
		if err := c.replaceWatchSet(ctx, newGvkWatchSet); err != nil {
			return fmt.Errorf("error watching new gvks: %w", err)
		}
	}

	return nil
}

func (c *CacheManager) replaceWatchSet(ctx context.Context, newWatchSet *watch.Set) error {
	// assumes caller has lock

	var innerError error
	c.watchedSet.Replace(newWatchSet, func() {
		// *Note the following steps are not transactional with respect to admission control

		// Important: dynamic watches update must happen *after* updating our watchSet.
		// Otherwise, the sync controller will drop events for the newly watched kinds.
		// Defer error handling so object re-sync happens even if the watch is hard
		// errored due to a missing GVK in the watch set.
		innerError = c.registrar.ReplaceWatch(ctx, newWatchSet.Items())
	})
	if innerError != nil {
		return innerError
	}

	return nil
}

func (c *CacheManager) RemoveSource(ctx context.Context, sourceKey aggregator.Key) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.gvkAggregator.Remove(sourceKey); err != nil {
		return fmt.Errorf("internal error removing source: %w", err)
	}
	// watchSet update will happen async-ly in manageCache

	return nil
}

func (c *CacheManager) ExcludeProcesses(newExcluder *process.Excluder) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.processExcluder.Equals(newExcluder) {
		return
	}

	c.processExcluder.Replace(newExcluder)
	// there is a new excluder which means we need to schedule a wipe for any
	// previously watched GVKs to be re-added to get a chance to be evaluated
	// for this new process excluder.
	c.excluderChanged = true
}

func (c *CacheManager) AddObject(ctx context.Context, instance *unstructured.Unstructured) error {
	// only perform work for watched gvks
	if gvk := instance.GroupVersionKind(); !c.watchedSet.Contains(gvk) {
		return nil
	}

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
	// only perform work for watched gvks
	if gvk := instance.GroupVersionKind(); !c.watchedSet.Contains(gvk) {
		return nil
	}

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

func (c *CacheManager) manageCache(ctx context.Context) {
	stopChan := make(chan bool, 1)
	gvkErrdChan := make(chan schema.GroupVersionKind, 1024)
	gvksFailingTolist := watch.NewSet()

	gvksFailingToListReconciler := func(stopChan <-chan bool) {
		for {
			select {
			case <-stopChan:
				return
			case gvk := <-gvkErrdChan:
				gvksFailingTolist.Add(gvk)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			close(stopChan)
			close(gvkErrdChan)
			return
		case <-c.cacheManagementTicker.C:
			c.mu.Lock()
			c.makeUpdates(ctx)

			// spin up new goroutines to relist if new gvks to relist are
			// populated from makeUpdates.
			if c.gvksToRelist.Size() != 0 {
				// stop any goroutines that were relisting before
				stopChan <- true

				// also try to catch any gvks that are in the aggregator
				// but are failing to list from a previous replay.
				for _, gvk := range gvksFailingTolist.Items() {
					if c.gvkAggregator.IsPresent(gvk) {
						c.gvksToRelist.Add(gvk)
					}
				}

				// save all gvks that need relisting
				gvksToRelistForLoop := c.gvksToRelist.Items()

				// clean state
				gvksFailingTolist = watch.NewSet()
				c.gvksToRelist = watch.NewSet()

				stopChan = make(chan bool)

				go c.replayLoop(ctx, gvksToRelistForLoop, stopChan)
				go gvksFailingToListReconciler(stopChan)
			}
			c.mu.Unlock()
		}
	}
}

func (c *CacheManager) replayLoop(ctx context.Context, gvksToRelist []schema.GroupVersionKind, stopChan <-chan bool) {
	for _, gvk := range gvksToRelist {
		select {
		case <-ctx.Done():
			return
		case <-stopChan:
			return
		default:
			backoff := wait.Backoff{
				Duration: time.Second,
				Factor:   2,
				Jitter:   0.1,
				Steps:    3,
			}

			operation := func() (bool, error) {
				if err := c.listAndSyncDataForGVK(ctx, gvk); err != nil {
					return false, err
				}

				return true, nil
			}

			if err := wait.ExponentialBackoff(backoff, operation); err != nil {
				log.Error(err, "internal: error listings gvk cache data", "gvk", gvk)
			}
		}
	}
}

// listAndSyncData returns a set of gvks that were successfully listed and synced.
func (c *CacheManager) listAndSyncData(ctx context.Context, gvks []schema.GroupVersionKind) *watch.Set {
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

// makeUpdates performs a conditional wipe followed by a replay if necessary as
// given by the current spec (currentGVKsInAgg, excluderChanged) at the time of the call.
func (c *CacheManager) makeUpdates(ctx context.Context) {
	// assumes the caller has lock

	currentGVKsInAgg := watch.NewSet()
	currentGVKsInAgg.Add(c.gvkAggregator.ListAllGVKs()...)

	if c.watchedSet.Equals(currentGVKsInAgg) && !c.excluderChanged {
		// nothing to do if both sets are the same and the excluder didn't change
		// and there are no gvks that need relisting from a previous wipe
		return
	}

	gvksToDelete := c.watchedSet.Difference(currentGVKsInAgg)
	newGVKsToSync := currentGVKsInAgg.Difference(c.watchedSet)
	gvksToReplay := c.watchedSet.Intersection(currentGVKsInAgg)

	if gvksToDelete.Size() != 0 || newGVKsToSync.Size() != 0 {
		// in this case we need to replace the watch set again since there
		// is drift between the aggregator and the currently watched gvks
		if err := c.replaceWatchSet(ctx, currentGVKsInAgg); err != nil {
			log.Error(err, "internal: error replacing watch set")
		}
	}

	// remove any gvks not needing to be synced anymore
	// or re evaluate all if the excluder changed.
	if gvksToDelete.Size() > 0 || c.excluderChanged {
		if err := c.wipeData(ctx); err != nil {
			log.Error(err, "internal: error wiping cache")
		} else {
			c.excluderChanged = false
		}
		c.gvksToRelist.AddSet(gvksToReplay)
	}
}
