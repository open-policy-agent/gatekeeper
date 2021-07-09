package process

import (
	"reflect"
	"sync"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// Process indicates the Gatekeeper component from which the resource will be excluded.
type Process string

// The set of defined Gatekeeper processes.
const (
	Audit    = Process("audit")
	Sync     = Process("sync")
	Webhook  = Process("webhook")
	Mutation = Process("mutation-webhook")
	Star     = Process("*")
)

type Excluder struct {
	mux                sync.RWMutex
	excludedNamespaces map[Process]map[util.PrefixWildcard]bool
}

var allProcesses = []Process{
	Audit,
	Webhook,
	Sync,
}

var processExcluder = &Excluder{
	excludedNamespaces: make(map[Process]map[util.PrefixWildcard]bool),
}

func Get() *Excluder {
	return processExcluder
}

func New() *Excluder {
	return &Excluder{
		excludedNamespaces: make(map[Process]map[util.PrefixWildcard]bool),
	}
}

func (s *Excluder) Add(entry []configv1alpha1.MatchEntry) {
	s.mux.Lock()
	defer s.mux.Unlock()

	for _, matchEntry := range entry {
		for _, ns := range matchEntry.ExcludedNamespaces {
			for _, op := range matchEntry.Processes {
				// adding excluded namespace to all processes for "*"
				if Process(op) == Star {
					for _, o := range allProcesses {
						if s.excludedNamespaces[o] == nil {
							s.excludedNamespaces[o] = make(map[util.PrefixWildcard]bool)
						}
						s.excludedNamespaces[o][ns] = true
					}
				} else {
					if s.excludedNamespaces[Process(op)] == nil {
						s.excludedNamespaces[Process(op)] = make(map[util.PrefixWildcard]bool)
					}
					s.excludedNamespaces[Process(op)][ns] = true
				}
			}
		}
	}
}

func (s *Excluder) Replace(new *Excluder) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.excludedNamespaces = new.excludedNamespaces
}

func (s *Excluder) Equals(new *Excluder) bool {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return reflect.DeepEqual(s.excludedNamespaces, new.excludedNamespaces)
}

func (s *Excluder) IsNamespaceExcluded(process Process, obj runtime.Object) (bool, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	meta, err := meta.Accessor(obj)
	if err != nil {
		return false, errors.Wrapf(err, "Failed to get accessor for %s - %s", obj.GetObjectKind().GroupVersionKind().Group, obj.GetObjectKind().GroupVersionKind().Kind)
	}

	if obj.GetObjectKind().GroupVersionKind().Kind == "Namespace" && obj.GetObjectKind().GroupVersionKind().Group == "" {
		return exactOrPrefixMatch(s.excludedNamespaces[process], meta.GetName()), nil
	}

	return exactOrPrefixMatch(s.excludedNamespaces[process], meta.GetNamespace()), nil
}

func exactOrPrefixMatch(boolMap map[util.PrefixWildcard]bool, ns string) bool {
	for k := range boolMap {
		if k.Matches(ns) {
			return true
		}
	}

	return false
}
