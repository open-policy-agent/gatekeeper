package watch

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type vitals struct {
	gvk        schema.GroupVersionKind
	registrars map[*Registrar]bool
}

func (w *vitals) merge(wv vitals) (vitals, error) {
	registrars := make(map[*Registrar]bool)
	for r := range w.registrars {
		registrars[r] = true
	}
	for r := range wv.registrars {
		registrars[r] = true
	}
	return vitals{
		gvk:        w.gvk,
		registrars: registrars,
	}, nil
}

func (w *vitals) addFns() []func(manager.Manager, schema.GroupVersionKind) error {
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
	intent     map[string]map[schema.GroupVersionKind]vitals
	intentMux  sync.RWMutex
	registrars map[string]*Registrar
	mgr        *Manager
}

func (r *recordKeeper) NewRegistrar(parentName string, addFns []func(manager.Manager, schema.GroupVersionKind) error) (*Registrar, error) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	if _, ok := r.registrars[parentName]; ok {
		return nil, fmt.Errorf("registrar for %s already exists", parentName)
	}
	addFnsCpy := make([]func(manager.Manager, schema.GroupVersionKind) error, len(addFns))
	copy(addFnsCpy, addFns)
	r.registrars[parentName] = &Registrar{
		parentName:   parentName,
		addFns:       addFnsCpy,
		mgr:          r.mgr,
		managedKinds: r,
	}
	return r.registrars[parentName], nil
}

func (r *recordKeeper) Update(u map[string]map[schema.GroupVersionKind]vitals) {
	r.intentMux.Lock()
	defer r.intentMux.Unlock()
	for k := range u {
		if _, ok := r.intent[k]; !ok {
			r.intent[k] = make(map[schema.GroupVersionKind]vitals)
		}
		for k2, v := range u[k] {
			r.intent[k][k2] = v
		}
	}
}

func (r *recordKeeper) ReplaceRegistrarRoster(reg *Registrar, roster map[schema.GroupVersionKind]vitals) {
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

func (r *recordKeeper) Get() (map[schema.GroupVersionKind]vitals, error) {
	r.intentMux.RLock()
	defer r.intentMux.RUnlock()
	cpy := make(map[string]map[schema.GroupVersionKind]vitals)
	for k := range r.intent {
		cpy[k] = make(map[schema.GroupVersionKind]vitals)
		for k2, v := range r.intent[k] {
			cpy[k][k2] = v
		}
	}
	managedKinds := make(map[schema.GroupVersionKind]vitals)
	for _, registrar := range cpy {
		for gvk, v := range registrar {
			if mk, ok := managedKinds[gvk]; ok {
				merged, err := mk.merge(v)
				if err != nil {
					return nil, err
				}
				managedKinds[gvk] = merged
			} else {
				managedKinds[gvk] = v
			}
		}
	}
	return managedKinds, nil
}

func (r *recordKeeper) GetGVK() ([]schema.GroupVersionKind, error) {
	var gvks []schema.GroupVersionKind

	g, err := r.Get()
	if err != nil {
		return nil, err
	}
	for gvk := range g {
		gvks = append(gvks, gvk)
	}
	return gvks, nil
}

func newRecordKeeper() *recordKeeper {
	return &recordKeeper{
		intent:     make(map[string]map[schema.GroupVersionKind]vitals),
		registrars: make(map[string]*Registrar),
	}
}

// A Registrar allows a parent to add/remove child watches
type Registrar struct {
	parentName   string
	addFns       []func(manager.Manager, schema.GroupVersionKind) error
	mgr          *Manager
	managedKinds *recordKeeper
}

// AddWatch registers a watch for the given kind
func (r *Registrar) AddWatch(gvk schema.GroupVersionKind) error {
	wv := vitals{
		gvk:        gvk,
		registrars: map[*Registrar]bool{r: true},
	}
	r.managedKinds.Update(map[string]map[schema.GroupVersionKind]vitals{r.parentName: {gvk: wv}})
	return nil
}

func (r *Registrar) ReplaceWatch(gvks []schema.GroupVersionKind) error {
	roster := make(map[schema.GroupVersionKind]vitals)
	for _, gvk := range gvks {
		wv := vitals{
			gvk:        gvk,
			registrars: map[*Registrar]bool{r: true},
		}
		roster[gvk] = wv
	}
	r.managedKinds.ReplaceRegistrarRoster(r, roster)
	return nil
}

func (r *Registrar) RemoveWatch(gvk schema.GroupVersionKind) error {
	r.managedKinds.Remove(map[string][]schema.GroupVersionKind{r.parentName: {gvk}})
	return nil
}

func (r *Registrar) Pause() error {
	return r.mgr.Pause()
}

func (r *Registrar) Unpause() error {
	return r.mgr.Unpause()
}
