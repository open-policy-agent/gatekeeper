package match

import (
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
	ExcludedNamespaces map[Operation][]string
}

var allOperations = []Operation{
	Audit,
	Webhook,
	Sync,
}

var configMapSet = &Set{
	ExcludedNamespaces: make(map[Operation][]string),
}

func NewSet() *Set {
	return configMapSet
}

func (s *Set) Update(entry []configv1alpha1.MatchEntry) {
	for _, matchEntry := range entry {
		for _, ns := range matchEntry.ExcludedNamespaces {
			for _, op := range matchEntry.Operations {
				if Operation(op) == Star {
					for _, o := range allOperations {
						if !s.contains(o, ns) {
							s.ExcludedNamespaces[o] = append(s.ExcludedNamespaces[o], ns)
						}
					}
				} else {
					if !s.contains(Operation(op), ns) {
						s.ExcludedNamespaces[Operation(op)] = append(s.ExcludedNamespaces[Operation(op)], ns)
					}
				}
			}
		}
	}
}

func (s *Set) Reset() {
	configMapSet.ExcludedNamespaces = make(map[Operation][]string)
}

func (s *Set) contains(op Operation, val string) bool {
	for _, item := range s.ExcludedNamespaces[op] {
		if item == val {
			return true
		}
	}
	return false
}
