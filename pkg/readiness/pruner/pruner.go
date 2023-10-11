package pruner

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
)

const tickDuration = 3 * time.Second

// ExpectationsPruner fires after sync expectations have been satisfied in the ready Tracker and runs
// until the overall Tracker is satisfied. It removes Data expectations for any
// GVKs that are expected in the Tracker but not watched by the CacheManager.
type ExpectationsPruner struct {
	cacheMgr *cachemanager.CacheManager
	tracker  *readiness.Tracker
}

func NewExpecationsPruner(cm *cachemanager.CacheManager, rt *readiness.Tracker) *ExpectationsPruner {
	return &ExpectationsPruner{
		cacheMgr: cm,
		tracker:  rt,
	}
}

func (e *ExpectationsPruner) Run(ctx context.Context) error {
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
			if !e.tracker.SyncSourcesSatisfied() {
				// not yet ready to prune data expectations.
				break
			}

			watchedGVKs := watch.NewSet()
			watchedGVKs.Add(e.cacheMgr.WatchedGVKs()...)
			expectedGVKs := watch.NewSet()
			expectedGVKs.Add(e.tracker.DataGVKs()...)

			for _, gvk := range expectedGVKs.Difference(watchedGVKs).Items() {
				e.tracker.CancelData(gvk)
			}
		}
	}
}
