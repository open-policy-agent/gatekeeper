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
	mux                sync.RWMutex
	ExcludedNamespaces map[Operation]map[string]bool
}

var allOperations = []Operation{
	Audit,
	Webhook,
	Sync,
}

var configMapSet = &Set{
	ExcludedNamespaces: make(map[Operation]map[string]bool),
}

func GetSet() *Set {
	return configMapSet
}

func newSet() *Set {
	return &Set{
		ExcludedNamespaces: make(map[Operation]map[string]bool),
	}
}

func (s *Set) update(entry []configv1alpha1.MatchEntry) {
	s.mux.Lock()
	defer s.mux.Unlock()

	for _, matchEntry := range entry {
		for _, ns := range matchEntry.ExcludedNamespaces {
			for _, op := range matchEntry.Operations {
				// adding excluded namespace to all operations for "*"
				if Operation(op) == Star {
					for _, o := range allOperations {
						if s.ExcludedNamespaces[o] == nil {
							s.ExcludedNamespaces[o] = make(map[string]bool)
						}
						s.ExcludedNamespaces[o][ns] = true
					}
				} else {
					if s.ExcludedNamespaces[Operation(op)] == nil {
						s.ExcludedNamespaces[Operation(op)] = make(map[string]bool)
					}
					s.ExcludedNamespaces[Operation(op)][ns] = true
				}
			}
		}
	}
}

func (s *Set) Replace(entry []configv1alpha1.MatchEntry) {
	s.mux.Lock()
	defer s.mux.Unlock()

	newConfigMapSet := newSet()
	newConfigMapSet.update(entry)

	s.ExcludedNamespaces = newConfigMapSet.ExcludedNamespaces
}
