package watch

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	errp "github.com/pkg/errors"
	apiErr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logf.Log.WithName("watchManager")

// WatchManager allows us to dynamically configure what kinds are watched
type Manager struct {
	newMgrFn   func(*Manager) (manager.Manager, error)
	startedMux sync.RWMutex
	stopper    func()
	stopped    chan struct{}
	// started is a bool (which is not thread-safe by default)
	started atomic.Value
	paused  bool
	// managedKinds stores the kinds that should be managed, mapping CRD Kind to CRD Name
	managedKinds *recordKeeper
	// watchedKinds are the kinds that have a currently running constraint controller
	watchedKinds map[schema.GroupVersionKind]vitals
	cfg          *rest.Config
	newDiscovery func(*rest.Config) (Discovery, error)
	metrics      *reporter
}

type Discovery interface {
	ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error)
}

// newDiscovery gets around the lack of interface inference when analyzing function signatures
func newDiscovery(c *rest.Config) (Discovery, error) {
	return discovery.NewDiscoveryClientForConfig(c)
}

func New(cfg *rest.Config) (*Manager, error) {
	metrics, err := newStatsReporter()
	if err != nil {
		return nil, err
	}
	wm := &Manager{
		newMgrFn:     newMgr,
		stopper:      func() {},
		managedKinds: newRecordKeeper(),
		watchedKinds: make(map[schema.GroupVersionKind]vitals),
		cfg:          cfg,
		newDiscovery: newDiscovery,
		metrics:      metrics,
	}
	wm.started.Store(false)
	wm.managedKinds.mgr = wm
	return wm, nil
}

func (wm *Manager) NewRegistrar(parent string, addFns []func(manager.Manager, schema.GroupVersionKind) error) (*Registrar, error) {
	return wm.managedKinds.NewRegistrar(parent, addFns)
}

func newMgr(wm *Manager) (manager.Manager, error) {
	log.Info("setting up watch manager")
	mgr, err := manager.New(wm.cfg, manager.Options{MetricsBindAddress: "0"})
	if err != nil {
		log.Error(err, "unable to set up watch manager")
		os.Exit(1)
	}

	return mgr, nil
}

// updateManager scans for changes to the watch list and restarts the manager if any are detected
func (wm *Manager) updateManager() (bool, error) {
	intent, err := wm.managedKinds.Get()
	if err != nil {
		return false, errp.Wrap(err, "error while retrieving managedKinds, not restarting watch manager")
	}
	if err := wm.metrics.reportGvkIntentCount(int64(len(intent))); err != nil {
		log.Error(err, "while reporting gvk intent count metric")
	}
	added, removed, changed, err := wm.gatherChanges(intent)
	if err != nil {
		return false, errp.Wrap(err, "error gathering watch changes, not restarting watch manager")
	}
	started := wm.started.Load().(bool)
	if started && len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		return false, nil
	}
	var a, r, c []string
	for k := range added {
		a = append(a, k.String())
	}
	for k := range removed {
		r = append(r, k.String())
	}
	for k := range changed {
		a = append(c, k.String())
	}
	log.Info("Watcher registry found changes and/or needs restarting", "started", started, "add", a, "remove", r, "change", c)

	readyToAdd, err := wm.filterPendingResources(added)
	if err != nil {
		return false, errp.Wrap(err, "could not filter pending resources, not restarting watch manager")
	}

	if started && len(readyToAdd) == 0 && len(removed) == 0 && len(changed) == 0 {
		log.Info("Only changes are pending additions; not restarting watch manager")
		return false, nil
	}

	newWatchedKinds := make(map[schema.GroupVersionKind]vitals)
	for gvk, vitals := range wm.watchedKinds {
		if _, ok := removed[gvk]; !ok {
			if newVitals, ok := changed[gvk]; ok {
				newWatchedKinds[gvk] = newVitals
			} else {
				newWatchedKinds[gvk] = vitals
			}
		}
	}

	filteredNewWatchedKinds, err := wm.filterPendingResources(newWatchedKinds)
	if err != nil {
		return false, errp.Wrap(err, "could not filter new watched kinds, not restarting watch manager")
	}
	if len(filteredNewWatchedKinds) != len(newWatchedKinds) {
		var missing []string
		for k := range newWatchedKinds {
			if _, ok := filteredNewWatchedKinds[k]; !ok {
				missing = append(missing, k.String())
			}
		}
		log.Info("previously watched resources have gone pending, removing them from the watch list", "pending", missing)
	}

	for gvk, vitals := range readyToAdd {
		filteredNewWatchedKinds[gvk] = vitals
	}

	if err := wm.restartManager(filteredNewWatchedKinds); err != nil {
		return false, errp.Wrap(err, "could not restart watch manager: %s")
	}

	wm.watchedKinds = filteredNewWatchedKinds
	return true, nil
}

// Start looks for changes to the watch roster every 5 seconds. This method has a
// benefit compared to restarting the manager every time a controller changes the watch
// of placing an upper bound on how often the manager restarts.
func (wm *Manager) Start(done <-chan struct{}) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			log.Info("watch manager shutting down")
			wm.close()
			return nil
		case <-ticker.C:
			if _, err := wm.updateOrPause(); err != nil {
				log.Error(err, "error in updateManagerLoop")
			}
		}
	}
}

// updateOrPause() wraps the update function, allowing us to check if the manager is paused in
// a thread-safe manner
func (wm *Manager) updateOrPause() (bool, error) {
	wm.startedMux.Lock()
	defer wm.startedMux.Unlock()
	// Report restart check after acquiring the lock so that we can detect deadlocks
	if err := wm.metrics.reportRestartCheck(); err != nil {
		log.Error(err, "while trying to report restart check metric")
	}
	if wm.paused {
		log.Info("update manager is paused")
		return false, nil
	}
	return wm.updateManager()
}

// Pause the manager to prevent syncing while other things are happening, such as wiping
// the data cache
func (wm *Manager) Pause() error {
	wm.startedMux.Lock()
	defer wm.startedMux.Unlock()
	if wm.stopped != nil {
		wm.stopper()
		select {
		case <-wm.stopped:
		case <-time.After(10 * time.Second):
			return errors.New("timeout waiting for watch manager to pause")
		}
	}
	wm.paused = true
	return nil
}

// Unpause the manager and start new watches
func (wm *Manager) Unpause() error {
	wm.startedMux.Lock()
	defer wm.startedMux.Unlock()
	wm.paused = false
	return nil
}

// restartManager destroys the old manager and creates a new one watching the provided constraint
// kinds
func (wm *Manager) restartManager(kinds map[schema.GroupVersionKind]vitals) error {
	var kindStr []string
	for gvk := range kinds {
		kindStr = append(kindStr, gvk.String())
	}
	log.Info("restarting Watch Manager", "kinds", strings.Join(kindStr, ", "))
	wm.stopper()
	// Only block on the old manager's exit if one has previously been started
	if wm.stopped != nil {
		<-wm.stopped
	}

	mgr, err := wm.newMgrFn(wm)
	if err != nil {
		return err
	}

	for gvk, v := range kinds {
		for _, fn := range v.addFns() {
			if err := fn(mgr, gvk); err != nil {
				return err
			}
		}
	}

	// reporting the restart after all potentially blocking calls will help narrow
	// down the cause of any deadlocks by checking if last_restart > last_restart_check
	if err := wm.metrics.reportRestart(); err != nil {
		log.Error(err, "while trying to report restart metric")
	}
	wm.stopped = make(chan struct{})
	stopper := make(chan struct{})
	stopOnce := sync.Once{}
	wm.stopper = func() {
		stopOnce.Do(func() { close(stopper) })
	}
	go wm.startMgr(mgr, stopper, wm.stopped, kindStr)
	return nil
}

func (wm *Manager) startMgr(mgr manager.Manager, stopper chan struct{}, stopped chan<- struct{}, kinds []string) {
	defer wm.started.Store(false)
	defer close(stopped)
	if err := wm.metrics.reportIsRunning(1); err != nil {
		log.Error(err, "while trying to report running metric")
	}
	defer func() {
		if err := wm.metrics.reportIsRunning(0); err != nil {
			log.Error(err, "while trying to report stopped metric")
		}
	}()
	if err := wm.metrics.reportGvkCount(int64(len(kinds))); err != nil {
		log.Error(err, "while trying to report gvk count metric")
	}
	log.Info("Calling Manager.Start()", "kinds", kinds)
	wm.started.Store(true)
	if err := mgr.Start(stopper); err != nil {
		log.Error(err, "error starting watch manager")
	}
	// mgr.Start() only returns after the manager has completely stopped
	log.Info("sub-manager exiting", "kinds", kinds)
}

// gatherChanges returns anything added, removed or changed since the last time the manager
// was successfully started. It also returns any errors gathering the changes.
func (wm *Manager) gatherChanges(managedKinds map[schema.GroupVersionKind]vitals) (map[schema.GroupVersionKind]vitals, map[schema.GroupVersionKind]vitals, map[schema.GroupVersionKind]vitals, error) {
	added := make(map[schema.GroupVersionKind]vitals)
	removed := make(map[schema.GroupVersionKind]vitals)
	changed := make(map[schema.GroupVersionKind]vitals)
	for gvk, vitals := range managedKinds {
		if _, ok := wm.watchedKinds[gvk]; !ok {
			added[gvk] = vitals
		}
	}
	for gvk, vitals := range wm.watchedKinds {
		if _, ok := managedKinds[gvk]; !ok {
			removed[gvk] = vitals
			continue
		}
		if !reflect.DeepEqual(wm.watchedKinds[gvk].registrars, managedKinds[gvk].registrars) {
			changed[gvk] = managedKinds[gvk]
		}
	}
	return added, removed, changed, nil
}

func (wm *Manager) filterPendingResources(kinds map[schema.GroupVersionKind]vitals) (map[schema.GroupVersionKind]vitals, error) {
	gvs := make(map[schema.GroupVersion]bool)
	for gvk := range kinds {
		gvs[gvk.GroupVersion()] = true
	}

	discovery, err := wm.newDiscovery(wm.cfg)
	if err != nil {
		return nil, err
	}
	liveResources := make(map[schema.GroupVersionKind]vitals)
	for gv := range gvs {
		rsrs, err := discovery.ServerResourcesForGroupVersion(gv.String())
		if err != nil {
			if e, ok := err.(*apiErr.StatusError); ok {
				if e.ErrStatus.Reason == metav1.StatusReasonNotFound {
					log.Info("skipping non-existent groupVersion", "groupVersion", gv.String())
					continue
				}
			}
			return nil, err
		}
		for _, r := range rsrs.APIResources {
			gvk := gv.WithKind(r.Kind)
			if wv, ok := kinds[gvk]; ok {
				liveResources[gvk] = wv
			}
		}
	}
	return liveResources, nil
}

func (wm *Manager) close() {
	log.Info("attempting to stop watch manager...")
	wm.startedMux.Lock()
	defer wm.startedMux.Unlock()
	wm.stopper()
	log.Info("waiting for watch manager to shut down")
	if wm.stopped != nil {
		<-wm.stopped
	}
	log.Info("watch manager finished shutting down")
}

func (wm *Manager) GetManagedGVK() ([]schema.GroupVersionKind, error) {
	return wm.managedKinds.GetGVK()
}
