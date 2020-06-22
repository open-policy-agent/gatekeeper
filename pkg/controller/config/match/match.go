package match

import (
	"sync"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
)

type Process string

const (
	Audit   = Process("audit")
	Sync    = Process("sync")
	Webhook = Process("webhook")
	Star    = Process("*")
)

type Set struct {
	Mux                sync.RWMutex
	excludedNamespaces map[Process]map[string]bool
}

var allProcesses = []Process{
	Audit,
	Webhook,
	Sync,
}

var configMapSet = &Set{
	excludedNamespaces: make(map[Process]map[string]bool),
}

func GetSet() *Set {
	return configMapSet
}

func newSet() *Set {
	return &Set{
		excludedNamespaces: make(map[Process]map[string]bool),
	}
}

func (s *Set) update(entry []configv1alpha1.MatchEntry) {
	s.Mux.RLock()
	defer s.Mux.RUnlock()

	for _, matchEntry := range entry {
		for _, ns := range matchEntry.ExcludedNamespaces {
			for _, op := range matchEntry.Operations {
				// adding excluded namespace to all operations for "*"
				if Process(op) == Star {
					for _, o := range allProcesses {
						if s.excludedNamespaces[o] == nil {
							s.excludedNamespaces[o] = make(map[string]bool)
						}
						s.excludedNamespaces[o][ns] = true
					}
				} else {
					if s.excludedNamespaces[Process(op)] == nil {
						s.excludedNamespaces[Process(op)] = make(map[string]bool)
					}
					s.excludedNamespaces[Process(op)][ns] = true
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

func (s *Set) GetExcludedNamespaces(process Process) map[string]bool {
	s.Mux.RLock()
	defer s.Mux.RUnlock()

	out := make(map[string]bool)
	for k, v := range s.excludedNamespaces[process] {
		out[k] = v
	}

	return out
}
