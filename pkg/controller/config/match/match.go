package match

import (
	"sync"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
)

type Operation string

const (
	Audit   = Operation("audit")
	Sync    = Operation("sync")
	Webhook = Operation("webhook")
	Star    = Operation("*")
)

type Set struct {
	Mux                sync.RWMutex
	excludedNamespaces map[Operation]map[string]bool
}

var allOperations = []Operation{
	Audit,
	Webhook,
	Sync,
}

var configMapSet = &Set{
	excludedNamespaces: make(map[Operation]map[string]bool),
}

func GetSet() *Set {
	return configMapSet
}

func newSet() *Set {
	return &Set{
		excludedNamespaces: make(map[Operation]map[string]bool),
	}
}

func (s *Set) update(entry []configv1alpha1.MatchEntry) {
	s.Mux.RLock()
	defer s.Mux.RUnlock()

	for _, matchEntry := range entry {
		for _, ns := range matchEntry.ExcludedNamespaces {
			for _, op := range matchEntry.Operations {
				// adding excluded namespace to all operations for "*"
				if Operation(op) == Star {
					for _, o := range allOperations {
						if s.excludedNamespaces[o] == nil {
							s.excludedNamespaces[o] = make(map[string]bool)
						}
						s.excludedNamespaces[o][ns] = true
					}
				} else {
					if s.excludedNamespaces[Operation(op)] == nil {
						s.excludedNamespaces[Operation(op)] = make(map[string]bool)
					}
					s.excludedNamespaces[Operation(op)][ns] = true
				}
			}
		}
	}
}

func (s *Set) Replace(entry []configv1alpha1.MatchEntry) {
	s.Mux.RLock()
	defer s.Mux.RUnlock()

	newConfigMapSet := newSet()
	newConfigMapSet.update(entry)

	s.excludedNamespaces = newConfigMapSet.excludedNamespaces
}

func (s *Set) GetExcludedNamespaces(operation Operation) map[string]bool {
	return s.excludedNamespaces[operation]
}
