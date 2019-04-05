package watch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	errp "github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("watchManager")

// WatchManager allows us to dynamically configure what kinds are watched
type WatchManager struct {
	newMgrFn   func(*WatchManager) (manager.Manager, error)
	startedMux sync.RWMutex
	stopper    chan struct{}
	stopped    chan struct{}
	started    bool
	paused     bool
	// managedKinds stores the kinds that should be managed, mapping CRD Kind to CRD Name
	managedKinds *recordKeeper
	// watchedKinds are the kinds that have a currently running constraint controller
	watchedKinds map[schema.GroupVersionKind]watchVitals
	cfg          *rest.Config
	newDiscovery func(*rest.Config) (Discovery, error)
}

type Discovery interface {
	ServerResourcesForGroupVersion(string) (*metav1.APIResourceList, error)
}

// newDiscovery gets around the lack of interface inference when analyzing function signatures
func newDiscovery(c *rest.Config) (Discovery, error) {
	return discovery.NewDiscoveryClientForConfig(c)
}

func New(ctx context.Context, cfg *rest.Config) *WatchManager {
	wm := &WatchManager{
		newMgrFn:     newMgr,
		stopper:      make(chan struct{}),
		stopped:      make(chan struct{}),
		managedKinds: newRecordKeeper(),
		watchedKinds: make(map[schema.GroupVersionKind]watchVitals),
		cfg:          cfg,
		newDiscovery: newDiscovery,
	}
	wm.managedKinds.mgr = wm
	go wm.updateManagerLoop(ctx)
	return wm
}

func (wm *WatchManager) NewRegistrar(parent string, addFns []func(manager.Manager, schema.GroupVersionKind) error) (*Registrar, error) {
	return wm.managedKinds.NewRegistrar(parent, addFns)
}

func newMgr(wm *WatchManager) (manager.Manager, error) {
	log.Info("setting up watch manager")
	mgr, err := manager.New(wm.cfg, manager.Options{})
	if err != nil {
		log.Error(err, "unable to set up watch manager")
		os.Exit(1)
	}

	return mgr, nil
}

func (wm *WatchManager) addWatch(gvk schema.GroupVersionKind, registrar *Registrar) error {
	wv := watchVitals{
		gvk:        gvk,
		registrars: map[*Registrar]bool{registrar: true},
	}
	wm.managedKinds.Update(map[string]map[schema.GroupVersionKind]watchVitals{registrar.parentName: {gvk: wv}})
	return nil
}

func (wm *WatchManager) replaceWatchSet(gvks []schema.GroupVersionKind, registrar *Registrar) error {
	roster := make(map[schema.GroupVersionKind]watchVitals)
	for _, gvk := range gvks {
		wv := watchVitals{
			gvk:        gvk,
			registrars: map[*Registrar]bool{registrar: true},
		}
		roster[gvk] = wv
	}
	wm.managedKinds.ReplaceRegistrarRoster(registrar, roster)
	return nil
}

func (wm *WatchManager) removeWatch(gvk schema.GroupVersionKind, registrar *Registrar) error {
	wm.managedKinds.Remove(map[string][]schema.GroupVersionKind{registrar.parentName: {gvk}})
	return nil
}

// updateManager scans for changes to the watch list and restarts the manager if any are detected
func (wm *WatchManager) updateManager() (bool, error) {
	intent := wm.managedKinds.Get()
	added, removed, changed, err := wm.gatherChanges(intent)
	if err != nil {
		return false, errp.Wrap(err, "error gathering watch changes, not restarting watch manager")
	}
	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		return false, nil
	}
	var a, r, c []string
	for k, _ := range added {
		a = append(a, k.String())
	}
	for k, _ := range removed {
		r = append(r, k.String())
	}
	for k, _ := range changed {
		a = append(c, k.String())
	}
	log.Info("Watcher registry found changes, attempting to apply them", "add", a, "remove", r, "change", c)

	readyToAdd, err := wm.filterPendingResources(added)
	if err != nil {
		return false, errp.Wrap(err, "could not filter pending resources, not restarting watch manager")
	}

	if len(readyToAdd) == 0 && len(removed) == 0 && len(changed) == 0 {
		log.Info("no resources ready to watch and nothing to remove")
		return false, nil
	}

	newWatchedKinds := make(map[schema.GroupVersionKind]watchVitals)
	for gvk, vitals := range wm.watchedKinds {
		if _, ok := removed[gvk]; !ok {
			if newVitals, ok := changed[gvk]; ok {
				newWatchedKinds[gvk] = newVitals
			} else {
				newWatchedKinds[gvk] = vitals
			}
		}
	}

	for gvk, vitals := range readyToAdd {
		newWatchedKinds[gvk] = vitals
	}

	if err := wm.restartManager(newWatchedKinds); err != nil {
		return false, errp.Wrap(err, "could not restart watch manager: %s")
	}

	wm.watchedKinds = newWatchedKinds
	return true, nil
}

// updateManagerLoop looks for changes to the watch roster every 5 seconds. This method has a dual
// benefit compared to restarting the manager every time a controller changes the watch
// of placing an upper bound on how often the manager restarts and allowing the manager to
// catch changes to the CRD version, will break the watch if it is not updated.
func (wm *WatchManager) updateManagerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			wm.Close()
			return
		default:
			time.Sleep(5 * time.Second)
			if wm.paused {
				continue
			}
			if _, err := wm.updateManager(); err != nil {
				log.Error(err, "error in updateManagerLoop")
			}
		}
	}
}

// updateOrPause() wraps the update function, allowing us to check if the manager is paused in
// a thread-safe manner
func (wm *WatchManager) updateOrPause() {
	wm.startedMux.Lock()
	defer wm.startedMux.Unlock()
	if wm.paused {
		log.Info("update manager is paused")
	}
	if _, err := wm.updateManager(); err != nil {
		log.Error(err, "error in updateManagerLoop")
	}
}

// Pause the manager to prevent syncing while other things are happening, such as wiping
// the data cache
func (wm *WatchManager) Pause() error {
	wm.startedMux.Lock()
	defer wm.startedMux.Unlock()
	if wm.started {
		close(wm.stopper)
		wm.stopper = make(chan struct{})
		select {
		case <-wm.stopped:
		case <-time.After(10 * time.Second):
			return errors.New("timeout waiting for watch manager to pause")
		}
	}
	wm.paused = true
	wm.started = false
	return nil
}

// Unpause the manager and start new watches
func (wm *WatchManager) Unpause() error {
	wm.startedMux.Lock()
	defer wm.startedMux.Unlock()
	wm.paused = false
	return nil
}

// restartManager destroys the old manager and creates a new one watching the provided constraint
// kinds
func (wm *WatchManager) restartManager(kinds map[schema.GroupVersionKind]watchVitals) error {
	var kindStr []string
	for gvk := range kinds {
		kindStr = append(kindStr, gvk.String())
	}
	log.Info("restarting Watch Manager", "kinds", strings.Join(kindStr, ", "))
	close(wm.stopper)
	// Only block on the old manager's exit if one has previously been started
	if wm.started {
		<-wm.stopped
	}

	wm.stopper = make(chan struct{})
	wm.stopped = make(chan struct{})
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

	wm.started = true
	go startMgr(mgr, wm.stopper, wm.stopped, kindStr)
	return nil
}

func startMgr(mgr manager.Manager, stopper chan struct{}, stopped chan<- struct{}, kinds []string) {
	log.Info("Calling Manager.Start()", "kinds", kinds)
	if err := mgr.Start(stopper); err != nil {
		log.Error(err, "error starting watch manager")
	}
	// mgr.Start() only returns after the manager has completely stopped
	close(stopped)
	log.Info("sub-manager exiting", "kinds", kinds)
}

// gatherChanges returns anything added, removed or changed since the last time the manager
// was successfully started. It also returns any errors gathering the changes.
func (wm *WatchManager) gatherChanges(managedKindsRaw map[string]map[schema.GroupVersionKind]watchVitals) (map[schema.GroupVersionKind]watchVitals, map[schema.GroupVersionKind]watchVitals, map[schema.GroupVersionKind]watchVitals, error) {
	managedKinds := make(map[schema.GroupVersionKind]watchVitals)
	for _, registrar := range managedKindsRaw {
		for gvk, v := range registrar {
			if mk, ok := managedKinds[gvk]; ok {
				merged, err := mk.merge(v)
				if err != nil {
					return nil, nil, nil, err
				}
				managedKinds[gvk] = merged
			} else {
				managedKinds[gvk] = v
			}
		}
	}

	added := make(map[schema.GroupVersionKind]watchVitals)
	removed := make(map[schema.GroupVersionKind]watchVitals)
	changed := make(map[schema.GroupVersionKind]watchVitals)
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
			// Do not clobber the newly added registrar
			vitals = managedKinds[gvk]
		}
	}
	return added, removed, changed, nil
}

func (wm *WatchManager) filterPendingResources(kinds map[schema.GroupVersionKind]watchVitals) (map[schema.GroupVersionKind]watchVitals, error) {
	gvs := make(map[schema.GroupVersion]bool)
	for gvk, _ := range kinds {
		gvs[gvk.GroupVersion()] = true
	}

	discovery, err := wm.newDiscovery(wm.cfg)
	if err != nil {
		return nil, err
	}
	liveResources := make(map[schema.GroupVersionKind]watchVitals)
	for gv, _ := range gvs {
		rsrs, err := discovery.ServerResourcesForGroupVersion(gv.String())
		if err != nil {
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

func (wm *WatchManager) Close() {
	close(wm.stopper)
}

func (wm *WatchManager) GetManaged() map[string]map[schema.GroupVersionKind]watchVitals {
	return wm.managedKinds.Get()
}

type watchVitals struct {
	gvk        schema.GroupVersionKind
	registrars map[*Registrar]bool
}

func (w *watchVitals) merge(wv watchVitals) (watchVitals, error) {
	registrars := make(map[*Registrar]bool)
	for r := range w.registrars {
		registrars[r] = true
	}
	for r := range wv.registrars {
		registrars[r] = true
	}
	return watchVitals{
		gvk:        w.gvk,
		registrars: registrars,
	}, nil
}

func (w *watchVitals) addFns() []func(manager.Manager, schema.GroupVersionKind) error {
	var addFns []func(manager.Manager, schema.GroupVersionKind) error
	for r := range w.registrars {
		addFns = append(addFns, r.addFns...)
	}
	return addFns
}

// recordKeeper holds the source of truth for the intended state of the manager
// This is essentially a read/write lock on the wrapped map (the `intent` variable)
type recordKeeper struct {
	// map[registrarName][kind]
	intent     map[string]map[schema.GroupVersionKind]watchVitals
	intentMux  sync.RWMutex
	registrars map[string]*Registrar
	mgr        *WatchManager
}

func (r *recordKeeper) NewRegistrar(parentName string, addFns []func(manager.Manager, schema.GroupVersionKind) error) (*Registrar, error) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	if _, ok := r.registrars[parentName]; ok {
		return nil, fmt.Errorf("registrar for %s already exists", parentName)
	}
	addFnsCpy := make([]func(manager.Manager, schema.GroupVersionKind) error, len(addFns))
	for i, v := range addFns {
		addFnsCpy[i] = v
	}
	r.registrars[parentName] = &Registrar{
		parentName: parentName,
		addFns:     addFnsCpy,
		mgr:        r.mgr,
	}
	return r.registrars[parentName], nil
}

func (r *recordKeeper) Update(u map[string]map[schema.GroupVersionKind]watchVitals) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	for k := range u {
		if _, ok := r.intent[k]; !ok {
			r.intent[k] = make(map[schema.GroupVersionKind]watchVitals)
		}
		for k2, v := range u[k] {
			r.intent[k][k2] = v
		}
	}
}

func (r *recordKeeper) ReplaceRegistrarRoster(reg *Registrar, roster map[schema.GroupVersionKind]watchVitals) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	r.intent[reg.parentName] = roster
}

func (r *recordKeeper) Remove(rm map[string][]schema.GroupVersionKind) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	for k, lst := range rm {
		for _, k2 := range lst {
			delete(r.intent[k], k2)
		}
	}
}

func (r *recordKeeper) Get() map[string]map[schema.GroupVersionKind]watchVitals {
	r.intentMux.RLock()
	defer r.intentMux.RUnlock()
	cpy := make(map[string]map[schema.GroupVersionKind]watchVitals)
	for k := range r.intent {
		cpy[k] = make(map[schema.GroupVersionKind]watchVitals)
		for k2, v := range r.intent[k] {
			cpy[k][k2] = v
		}
	}
	return cpy
}

func newRecordKeeper() *recordKeeper {
	return &recordKeeper{
		intent:     make(map[string]map[schema.GroupVersionKind]watchVitals),
		registrars: make(map[string]*Registrar),
	}
}

// A Registrar allows a parent to add/remove child watches
type Registrar struct {
	parentName string
	addFns     []func(manager.Manager, schema.GroupVersionKind) error
	mgr        *WatchManager
}

// AddWatch registers a watch for the given kind
func (r *Registrar) AddWatch(gvk schema.GroupVersionKind) error {
	return r.mgr.addWatch(gvk, r)
}

func (r *Registrar) ReplaceWatch(gvks []schema.GroupVersionKind) error {
	return r.mgr.replaceWatchSet(gvks, r)
}

func (r *Registrar) RemoveWatch(gvk schema.GroupVersionKind) error {
	return r.mgr.removeWatch(gvk, r)
}

func (r *Registrar) Pause() error {
	return r.mgr.Pause()
}

func (r *Registrar) Unpause() error {
	return r.mgr.Unpause()
}
