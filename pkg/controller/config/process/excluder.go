package process

import (
	"reflect"
	"sync"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	excludedNamespaces map[Process]map[wildcard.Wildcard]bool
}

var allProcesses = []Process{
	Audit,
	Webhook,
	Mutation,
	Sync,
}

var processExcluder = &Excluder{
	excludedNamespaces: make(map[Process]map[wildcard.Wildcard]bool),
}

func Get() *Excluder {
	return processExcluder
}

func New() *Excluder {
	return &Excluder{
		excludedNamespaces: make(map[Process]map[wildcard.Wildcard]bool),
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
							s.excludedNamespaces[o] = make(map[wildcard.Wildcard]bool)
						}
						s.excludedNamespaces[o][ns] = true
					}
				} else {
					if s.excludedNamespaces[Process(op)] == nil {
						s.excludedNamespaces[Process(op)] = make(map[wildcard.Wildcard]bool)
					}
					s.excludedNamespaces[Process(op)][ns] = true
				}
			}
		}
	}
}

func (s *Excluder) Replace(new *Excluder) { // nolint:revive
	s.mux.Lock()
	defer s.mux.Unlock()
	s.excludedNamespaces = new.excludedNamespaces
}

func (s *Excluder) Equals(new *Excluder) bool { // nolint:revive
	s.mux.RLock()
	defer s.mux.RUnlock()
	return reflect.DeepEqual(s.excludedNamespaces, new.excludedNamespaces)
}

func (s *Excluder) EqualsForProcess(process Process, new *Excluder) bool { // nolint:revive
	s.mux.RLock()
	defer s.mux.RUnlock()
	return reflect.DeepEqual(s.excludedNamespaces[process], new.excludedNamespaces[process])
}

func (s *Excluder) IsNamespaceExcluded(process Process, obj client.Object) (bool, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	if obj.GetObjectKind().GroupVersionKind().Kind == "Namespace" && obj.GetObjectKind().GroupVersionKind().Group == "" {
		return exactOrWildcardMatch(s.excludedNamespaces[process], obj.GetName()), nil
	}

	return exactOrWildcardMatch(s.excludedNamespaces[process], obj.GetNamespace()), nil
}

// GetExcludedNamespaces returns a list of excluded namespace patterns for the given process.
func (s *Excluder) GetExcludedNamespaces(process Process) []string {
	s.mux.RLock()
	defer s.mux.RUnlock()

	var excludedNamespaces []string
	for ns := range s.excludedNamespaces[process] {
		excludedNamespaces = append(excludedNamespaces, string(ns))
	}

	return excludedNamespaces
}

func exactOrWildcardMatch(boolMap map[wildcard.Wildcard]bool, ns string) bool {
	for k := range boolMap {
		if k.Matches(ns) {
			return true
		}
	}

	return false
}
