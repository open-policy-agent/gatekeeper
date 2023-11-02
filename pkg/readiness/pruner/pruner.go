package pruner

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
)

const tickDuration = 3 * time.Second

// ExpectationsPruner polls the ReadyTracker and other data sources in Gatekeeper to remove
// un-satisfiable expectations in the RT that would incorrectly block startup.
type ExpectationsPruner struct {
	cacheMgr *cachemanager.CacheManager
	tracker  *readiness.Tracker
}

func NewExpectationsPruner(cm *cachemanager.CacheManager, rt *readiness.Tracker) *ExpectationsPruner {
	return &ExpectationsPruner{
		cacheMgr: cm,
		tracker:  rt,
	}
}

func (e *ExpectationsPruner) Start(ctx context.Context) error {
	ticker := time.NewTicker(tickDuration)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if e.tracker.Satisfied() {
				// we're done, there's no need to
				// further manage the data sync expectations.
				return nil
			}
			if e.tracker.SyncSourcesSatisfied() {
				e.pruneUnwatchedGVKs()
			}
		}
	}
}

// pruneUnwatchedGVKs prunes data expectations that are no longer correct based on the up-to-date
// information in the CacheManager.
func (e *ExpectationsPruner) pruneUnwatchedGVKs() {
	watchedGVKs := watch.NewSet()
	watchedGVKs.Add(e.cacheMgr.WatchedGVKs()...)
	expectedGVKs := watch.NewSet()
	expectedGVKs.Add(e.tracker.DataGVKs()...)

	for _, gvk := range expectedGVKs.Difference(watchedGVKs).Items() {
		e.tracker.CancelData(gvk)
	}
}
