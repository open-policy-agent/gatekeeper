package process

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

type Excluder struct {
	Mux                sync.RWMutex
	excludedNamespaces map[Process]map[string]bool
}

var allProcesses = []Process{
	Audit,
	Webhook,
	Sync,
}

var configMapSet = &Excluder{
	excludedNamespaces: make(map[Process]map[string]bool),
}

func Get() *Excluder {
	return configMapSet
}

func new() *Excluder {
	return &Excluder{
		excludedNamespaces: make(map[Process]map[string]bool),
	}
}

func (s *Excluder) update(entry []configv1alpha1.MatchEntry) {
	s.Mux.Lock()
	defer s.Mux.Unlock()

	for _, matchEntry := range entry {
		for _, ns := range matchEntry.ExcludedNamespaces {
			for _, op := range matchEntry.Processes {
				// adding excluded namespace to all processes for "*"
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

func (s *Excluder) Replace(entry []configv1alpha1.MatchEntry) {
	s.Mux.Lock()
	defer s.Mux.Unlock()

	newConfigMapSet := new()
	newConfigMapSet.update(entry)

	s.excludedNamespaces = newConfigMapSet.excludedNamespaces
}

func (s *Excluder) getExcludedNamespaces(process Process) map[string]bool {
	s.Mux.RLock()
	defer s.Mux.RUnlock()

	out := make(map[string]bool)
	for k, v := range s.excludedNamespaces[process] {
		out[k] = v
	}

	return out
}

func (s *Excluder) IsNamespaceExcluded(process Process, namespace string) bool {
	excludedNS := s.getExcludedNamespaces(process)
	return excludedNS[namespace]
}
