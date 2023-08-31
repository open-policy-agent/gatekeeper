package cachemanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/aggregator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const RegistrarName = "cachemanager"

var (
	log     = logf.Log.WithName("cache-manager")
	backoff = wait.Backoff{
		Duration: time.Second,
		Factor:   2,
		Jitter:   0.1,
		Steps:    3,
	}
)

type Config struct {
	CfClient         CFDataClient
	SyncMetricsCache *syncutil.MetricsCache
	Tracker          *readiness.Tracker
	ProcessExcluder  *process.Excluder
	Registrar        *watch.Registrar
	GVKAggregator    *aggregator.GVKAgreggator
	Reader           client.Reader
}

type CacheManager struct {
	watchedSet            *watch.Set
	processExcluder       *process.Excluder
	gvksToSync            *aggregator.GVKAgreggator
	needToList            bool
	gvksToDeleteFromCache *watch.Set
	excluderChanged       bool
	// mu guards access to any of the fields above
	mu sync.RWMutex

	cfClient                   CFDataClient
	syncMetricsCache           *syncutil.MetricsCache
	tracker                    *readiness.Tracker
	registrar                  *watch.Registrar
	backgroundManagementTicker time.Ticker
	reader                     client.Reader
}

// CFDataClient is an interface for caching data.
type CFDataClient interface {
	AddData(ctx context.Context, data interface{}) (*types.Responses, error)
	RemoveData(ctx context.Context, data interface{}) (*types.Responses, error)
}

func NewCacheManager(config *Config) (*CacheManager, error) {
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

	if config.GVKAggregator == nil {
		config.GVKAggregator = aggregator.NewGVKAggregator()
	}

	cm := &CacheManager{
		cfClient:                   config.CfClient,
		syncMetricsCache:           config.SyncMetricsCache,
		tracker:                    config.Tracker,
		processExcluder:            config.ProcessExcluder,
		registrar:                  config.Registrar,
		watchedSet:                 watch.NewSet(),
		reader:                     config.Reader,
		gvksToSync:                 config.GVKAggregator,
		backgroundManagementTicker: *time.NewTicker(3 * time.Second),
		gvksToDeleteFromCache:      watch.NewSet(),
	}

	return cm, nil
}

func (c *CacheManager) Start(ctx context.Context) error {
	go c.manageCache(ctx)

	<-ctx.Done()
	return nil
}

// UpsertSource adjusts the watched set of gvks according to the newGVKs passed in
// for a given sourceKey. Callers are responsible for retrying on error.
func (c *CacheManager) UpsertSource(ctx context.Context, sourceKey aggregator.Key, newGVKs []schema.GroupVersionKind) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(newGVKs) > 0 {
		if err := c.gvksToSync.Upsert(sourceKey, newGVKs); err != nil {
			return fmt.Errorf("internal error adding source: %w", err)
		}
	} else {
		if err := c.gvksToSync.Remove(sourceKey); err != nil {
			return fmt.Errorf("internal error removing source: %w", err)
		}
	}

	// as a result of upserting the new gvks for the source key, some gvks
	// may become unreferenced and need to be deleted; this will be handled async
	// in the manageCache loop.

	if err := c.replaceWatchSet(ctx); err != nil {
		return fmt.Errorf("error watching new gvks: %w", err)
	}

	return nil
}

// replaceWatchSet looks at the gvksToSync and makes changes to the registrar's watch set.
// Assumes caller has lock. On error, actual watch state may not align with intended watch state.
func (c *CacheManager) replaceWatchSet(ctx context.Context) error {
	newWatchSet := watch.NewSet()
	newWatchSet.Add(c.gvksToSync.GVKs()...)

	diff := c.watchedSet.Difference(newWatchSet)
	c.removeStaleExpectations(diff)

	c.gvksToDeleteFromCache.AddSet(diff)

	var innerError error
	c.watchedSet.Replace(newWatchSet, func() {
		// *Note the following steps are not transactional with respect to admission control

		// Important: dynamic watches update must happen *after* updating our watchSet.
		// Otherwise, the sync controller will drop events for the newly watched kinds.
		innerError = c.registrar.ReplaceWatch(ctx, newWatchSet.Items())
	})

	return innerError
}

// removeStaleExpectations stops tracking data for any resources that are no longer watched.
func (c *CacheManager) removeStaleExpectations(stale *watch.Set) {
	for _, gvk := range stale.Items() {
		c.tracker.CancelData(gvk)
	}
}

// RemoveSource removes the watches of the GVKs for a given aggregator.Key. Callers are responsible for retrying on error.
func (c *CacheManager) RemoveSource(ctx context.Context, sourceKey aggregator.Key) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.gvksToSync.Remove(sourceKey); err != nil {
		return fmt.Errorf("internal error removing source: %w", err)
	}

	if err := c.replaceWatchSet(ctx); err != nil {
		return fmt.Errorf("error removing watches for source %v: %w", sourceKey, err)
	}

	return nil
}

// ExcludeProcesses swaps the current process excluder with the new *process.Excluder.
// It's a no-op if the two excluders are equal.
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

// DoForEach runs fn for each GVK that is being watched by the cache manager.
// This is handy when we want to take actions while holding the lock on the watched.Set.
func (c *CacheManager) DoForEach(fn func(gvk schema.GroupVersionKind) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	err := c.watchedSet.DoForEach(fn)
	return err
}

func (c *CacheManager) watchesGVK(gvk schema.GroupVersionKind) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.watchedSet.Contains(gvk)
}

func (c *CacheManager) AddObject(ctx context.Context, instance *unstructured.Unstructured) error {
	gvk := instance.GroupVersionKind()

	isNamespaceExcluded, err := c.processExcluder.IsNamespaceExcluded(process.Sync, instance)
	if err != nil {
		return fmt.Errorf("error while excluding namespaces for gvk: %+v: %w", gvk, err)
	}

	if isNamespaceExcluded {
		c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)
		return nil
	}

	syncKey := syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName())
	if c.watchesGVK(gvk) {
		_, err = c.cfClient.AddData(ctx, instance)
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

		c.syncMetricsCache.AddObject(syncKey, syncutil.Tags{
			Kind:   instance.GetKind(),
			Status: metrics.ActiveStatus,
		})
		c.syncMetricsCache.AddKind(instance.GetKind())
	}

	c.tracker.ForData(instance.GroupVersionKind()).Observe(instance)

	return nil
}

func (c *CacheManager) RemoveObject(ctx context.Context, instance *unstructured.Unstructured) error {
	if _, err := c.cfClient.RemoveData(ctx, instance); err != nil {
		return err
	}

	// only delete from metrics map if the data removal was successful
	c.syncMetricsCache.DeleteObject(syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName()))
	c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)

	return nil
}

func (c *CacheManager) wipeData(ctx context.Context) error {
	if _, err := c.cfClient.RemoveData(ctx, target.WipeData()); err != nil {
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

func (c *CacheManager) syncGVK(ctx context.Context, gvk schema.GroupVersionKind) error {
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	var err error
	func() {
		c.mu.RLock()
		defer c.mu.RUnlock()

		// only call List if we are still watching the gvk.
		if c.watchedSet.Contains(gvk) {
			err = c.reader.List(ctx, u)
		}
	}()

	if err != nil {
		return fmt.Errorf("listing data for %+v: %w", gvk, err)
	}

	for i := range u.Items {
		if err := c.AddObject(ctx, &u.Items[i]); err != nil {
			return fmt.Errorf("adding data for %+v: %w", gvk, err)
		}
	}

	return nil
}

func (c *CacheManager) manageCache(ctx context.Context) {
	// relistStopChan is used to stop any list operations still in progress
	relistStopChan := make(chan struct{})

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.backgroundManagementTicker.C:
			func() {
				c.mu.Lock()
				defer c.mu.Unlock()

				c.wipeCacheIfNeeded(ctx)

				if !c.needToList {
					// this means that there are no changes needed
					// such that any gvks need to be relisted.
					// any in flight goroutines can finish relisiting.
					return
				}

				// otherwise, spin up new goroutines to relist gvks as there has been a wipe

				// stop any goroutines that were relisting before
				// as we may no longer be interested in those gvks
				close(relistStopChan)

				// assume all gvks need to be relisted
				// and while under lock, make a copy of
				// all gvks so we can pass it in the goroutine
				// without needing to read lock this data
				gvksToRelist := c.gvksToSync.GVKs()

				// clean state
				c.needToList = false
				relistStopChan = make(chan struct{})

				go c.replayGVKs(ctx, gvksToRelist, relistStopChan)
			}()
		}
	}
}

func (c *CacheManager) replayGVKs(ctx context.Context, gvksToRelist []schema.GroupVersionKind, stopCh <-chan struct{}) {
	gvksSet := watch.NewSet()
	gvksSet.Add(gvksToRelist...)

	for gvksSet.Size() != 0 {
		gvkItems := gvksSet.Items()

		for _, gvk := range gvkItems {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			default:
				operation := func() (bool, error) {
					if err := c.syncGVK(ctx, gvk); err != nil {
						return false, err
					}
					return true, nil
				}

				if err := wait.ExponentialBackoff(backoff, operation); err != nil {
					log.Error(err, "internal: error listings gvk cache data", "gvk", gvk)
				} else {
					gvksSet.Remove(gvk)
				}
			}
		}

		c.ReportSyncMetrics()
	}
}

// wipeCacheIfNeeded performs a cache wipe if there are any gvks needing to be removed
// from the cache or if the excluder has changed. It also marks which gvks need to be
// re listed again in the cf data cache after the wipe. Assumes the caller has lock.
func (c *CacheManager) wipeCacheIfNeeded(ctx context.Context) {
	// remove any gvks not needing to be synced anymore
	// or re evaluate all if the excluder changed.
	if c.gvksToDeleteFromCache.Size() > 0 || c.excluderChanged {
		if err := c.wipeData(ctx); err != nil {
			log.Error(err, "internal: error wiping cache")
			return
		}

		c.gvksToDeleteFromCache = watch.NewSet()
		c.excluderChanged = false
		c.needToList = true
	}
}
