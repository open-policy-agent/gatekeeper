package watch

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	errp "github.com/pkg/errors"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("watchManager")

// WatchManager allows us to dynamically configure what kinds are watched
type WatchManager struct {
	client   client.Reader
	newMgrFn func(*WatchManager) (manager.Manager, error)
	stopper  chan struct{}
	stopped  chan struct{}
	started  bool
	// managedKinds stores the kinds that should be managed, mapping CRD Kind to CRD Name
	managedKinds *recordKeeper
	// watchedKinds are the kinds that have a currently running constraint controller
	watchedKinds map[string]watchVitals
	cfg          *rest.Config
}

func New(ctx context.Context, cfg *rest.Config, client client.Client) *WatchManager {
	wm := &WatchManager{
		client:       client,
		newMgrFn:     newMgr,
		stopper:      make(chan struct{}),
		stopped:      make(chan struct{}),
		managedKinds: newRecordKeeper(),
		watchedKinds: make(map[string]watchVitals),
		cfg:          cfg,
	}
	wm.managedKinds.mgr = wm
	go wm.updateManagerLoop(ctx)
	return wm
}

func (wm *WatchManager) NewRegistrar(parent string, addFns []func(manager.Manager, string) error) (*Registrar, error) {
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

func (wm *WatchManager) addWatch(kind, crdName string, registrar *Registrar) error {
	wv := watchVitals{
		kind:       kind,
		crdName:    crdName,
		registrars: map[*Registrar]bool{registrar: true},
	}
	wm.managedKinds.Update(map[string]map[string]watchVitals{registrar.parentName: {kind: wv}})
	return nil
}

func (wm *WatchManager) removeWatch(kind string, registrar *Registrar) error {
	wm.managedKinds.Remove(map[string][]string{registrar.parentName: {kind}})
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
	log.Info("Watcher registry found changes, attempting to apply them", "add", added, "remove", removed, "change", changed)

	readyToAdd, err := wm.filterPendingCRDs(added)
	if err != nil {
		return false, errp.Wrap(err, "could not filter pending CRDs, not restarting watch manager")
	}

	if len(readyToAdd) == 0 && len(removed) == 0 && len(changed) == 0 {
		log.Info("no resources ready to watch and nothing to remove")
		return false, nil
	}

	newWatchedKinds := make(map[string]watchVitals)
	for kind, vitals := range wm.watchedKinds {
		if _, ok := removed[kind]; !ok {
			if newVitals, ok := changed[kind]; ok {
				newWatchedKinds[kind] = newVitals
			} else {
				newWatchedKinds[kind] = vitals
			}
		}
	}

	for kind, vitals := range readyToAdd {
		newWatchedKinds[kind] = vitals
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
			if _, err := wm.updateManager(); err != nil {
				log.Error(err, "error in updateManagerLoop")
			}
		}
	}
}

// restartManager destroys the old manager and creates a new one watching the provided constraint
// kinds
func (wm *WatchManager) restartManager(kinds map[string]watchVitals) error {
	var kindStr []string
	for k := range kinds {
		kindStr = append(kindStr, k)
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

	for k, v := range kinds {
		for _, fn := range v.addFns() {
			if err := fn(mgr, k); err != nil {
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
func (wm *WatchManager) gatherChanges(managedKindsRaw map[string]map[string]watchVitals) (map[string]watchVitals, map[string]watchVitals, map[string]watchVitals, error) {
	managedKinds := make(map[string]watchVitals)
	for _, registrar := range managedKindsRaw {
		for k, v := range registrar {
			if mk, ok := managedKinds[k]; ok {
				merged, err := mk.merge(v)
				if err != nil {
					return nil, nil, nil, err
				}
				managedKinds[k] = merged
			} else {
				managedKinds[k] = v
			}
		}
	}

	added := make(map[string]watchVitals)
	removed := make(map[string]watchVitals)
	changed := make(map[string]watchVitals)
	for kind, vitals := range managedKinds {
		if _, ok := wm.watchedKinds[kind]; !ok {
			added[kind] = vitals
		}
	}
	for kind, vitals := range wm.watchedKinds {
		if _, ok := managedKinds[kind]; !ok {
			removed[kind] = vitals
			continue
		}
		instance, err := wm.getInstance(vitals.crdName)
		if err != nil {
			log.Info("could not gather watch vitals", "crdName", vitals.crdName)
			continue
		}
		if !reflect.DeepEqual(wm.watchedKinds[kind].registrars, managedKinds[kind].registrars) {
			changed[kind] = managedKinds[kind]
			// Do not clobber the newly added registrar
			vitals = managedKinds[kind]
		}
		if instance.GetResourceVersion() != vitals.version {
			vitals.version = instance.GetResourceVersion()
			changed[kind] = vitals
		}
	}
	return added, removed, changed, nil
}

func (wm *WatchManager) filterPendingCRDs(kinds map[string]watchVitals) (map[string]watchVitals, error) {
	liveCRDs := make(map[string]watchVitals)
	for kind, vitals := range kinds {
		// No CRD to check
		if vitals.crdName == "" {
			liveCRDs[kind] = vitals
			continue
		}
		found, err := wm.getInstance(vitals.crdName)
		if err != nil {
			if !errors.IsNotFound(err) {
				log.Error(err, "Could not retrieve CRD", "kind", kind, "crdName", vitals.crdName)
			}
			log.Info("CRD does not yet exist, skipping", "kind", kind)
			continue
		}
		if found.Status.AcceptedNames.Kind == "" {
			log.Info("CRD found, but no status, skipping.", "kind", kind, "crdName", vitals.crdName)
			continue
		}
		if found.Status.AcceptedNames.Kind != kind {
			log.Error(err, "unexpected accepted kind for CRD", "kind", kind, "crdName", vitals.crdName, "acceptedKind", found.Status.AcceptedNames.Kind)
			continue
		}
		log.Info("Kind is ready to be managed", "kind", kind)
		vitals.version = found.GetResourceVersion()
		liveCRDs[kind] = vitals
	}
	return liveCRDs, nil
}

func (wm *WatchManager) getInstance(name string) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
	found := &apiextensionsv1beta1.CustomResourceDefinition{}
	if err := wm.client.Get(context.TODO(), types.NamespacedName{Name: name}, found); err != nil {
		return nil, err
	}
	return found, nil
}

func (wm *WatchManager) Close() {
	close(wm.stopper)
}

type watchVitals struct {
	kind       string
	crdName    string
	version    string
	registrars map[*Registrar]bool
}

func (w *watchVitals) merge(wv watchVitals) (watchVitals, error) {
	crdName := w.crdName
	if w.crdName != wv.crdName {
		if w.crdName == "" {
			crdName = wv.crdName
		} else if wv.crdName != "" {
			return watchVitals{}, fmt.Errorf("mismatched CRD name for watch on %s", w.kind)
		}
	}
	registrars := make(map[*Registrar]bool)
	for r := range w.registrars {
		registrars[r] = true
	}
	for r := range wv.registrars {
		registrars[r] = true
	}
	return watchVitals{
		kind:       w.kind,
		crdName:    crdName,
		version:    w.version,
		registrars: registrars,
	}, nil
}

func (w *watchVitals) addFns() []func(manager.Manager, string) error {
	var addFns []func(manager.Manager, string) error
	for r := range w.registrars {
		addFns = append(addFns, r.addFns...)
	}
	return addFns
}

// recordKeeper holds the source of truth for the intended state of the manager
// This is essentially a read/write lock on the wrapped map (the `intent` variable)
type recordKeeper struct {
	// map[registrarName][kind]
	intent     map[string]map[string]watchVitals
	intentMux  sync.RWMutex
	registrars map[string]*Registrar
	mgr        *WatchManager
}

func (r *recordKeeper) NewRegistrar(parentName string, addFns []func(manager.Manager, string) error) (*Registrar, error) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	if _, ok := r.registrars[parentName]; ok {
		return nil, fmt.Errorf("registrar for %s already exists", parentName)
	}
	addFnsCpy := make([]func(manager.Manager, string) error, len(addFns))
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

func (r *recordKeeper) Update(u map[string]map[string]watchVitals) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	for k := range u {
		if _, ok := r.intent[k]; !ok {
			r.intent[k] = make(map[string]watchVitals)
		}
		for k2, v := range u[k] {
			r.intent[k][k2] = v
		}
	}
}

func (r *recordKeeper) Remove(rm map[string][]string) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	for k, lst := range rm {
		for _, k2 := range lst {
			delete(r.intent[k], k2)
		}
	}
}

func (r *recordKeeper) Get() map[string]map[string]watchVitals {
	r.intentMux.RLock()
	defer r.intentMux.RUnlock()
	cpy := make(map[string]map[string]watchVitals)
	for k := range r.intent {
		cpy[k] = make(map[string]watchVitals)
		for k2, v := range r.intent[k] {
			cpy[k][k2] = v
		}
	}
	return cpy
}

func newRecordKeeper() *recordKeeper {
	return &recordKeeper{
		intent:     make(map[string]map[string]watchVitals),
		registrars: make(map[string]*Registrar),
	}
}

// A Registrar allows a parent to add/remove child watches
type Registrar struct {
	parentName string
	addFns     []func(manager.Manager, string) error
	mgr        *WatchManager
}

// AddWatch registers a watch for the given kind.
// If crdName is defined, the CRD will be queried to make sure the resource
// is active before initiating the watch.
func (r *Registrar) AddWatch(kind, crdName string) error {
	return r.mgr.addWatch(kind, crdName, r)
}

func (r *Registrar) RemoveWatch(kind string) error {
	return r.mgr.removeWatch(kind, r)
}
