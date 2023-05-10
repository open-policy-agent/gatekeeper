package syncutil

import (
	"encoding/json"
	"strings"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// syncAnnotationName is the name of the annotation that stores
// GVKS that are required to be synced.
const SyncAnnotationName = "metadata.gatekeeper.sh/requiresSyncData"

// SyncRequirements contains a list of ANDed requirements, each of which
// contains an expanded set of equivalent (ORed) GVKs.
type SyncRequirements []GVKEquivalenceSet

// GVKEquivalenceSet is a set of GVKs that a template can use
// interchangeably in its referential policy implementation.
type GVKEquivalenceSet map[schema.GroupVersionKind]struct{}

// CompactSyncRequirements contains a list of ANDed requirements, each of
type CompactSyncRequirements [][]CompactGVKEquivalenceSet

// compactGVKEquivalenceSet contains a set of equivalent GVKs, expressed
// in the compact form [groups, versions, kinds] where any combination of
// items from these three fields can be considered a valid equivalent.
// Used solely for unmarshalling.
type CompactGVKEquivalenceSet struct {
	Groups   []string `json:"groups"`
	Versions []string `json:"versions"`
	Kinds    []string `json:"kinds"`
}

// ReadSyncRequirements parses the sync requirements from a
// constraint template.
func ReadSyncRequirements(t *templates.ConstraintTemplate) (SyncRequirements, error) {
	if t.ObjectMeta.Annotations != nil {
		if annotation, exists := t.ObjectMeta.Annotations[SyncAnnotationName]; exists {
			annotation = strings.Trim(annotation, "\n\"")
			compactSyncRequirements := CompactSyncRequirements{}
			decoder := json.NewDecoder(strings.NewReader(annotation))
			decoder.DisallowUnknownFields()
			err := decoder.Decode(&compactSyncRequirements)
			if err != nil {
				return nil, err
			}
			requirements, err := ExpandCompactRequirements(compactSyncRequirements)
			if err != nil {
				return nil, err
			}
			return requirements, nil
		}
	}
	return SyncRequirements{}, nil
}

// Takes a compactGVKSet and expands and unions it with the set of
// GVKs pointed to by the 'expandedEquivalentSet' argument.
func ExpandCompactEquivalenceSet(compactEquivalenceSet CompactGVKEquivalenceSet) GVKEquivalenceSet {
	equivalenceSet := GVKEquivalenceSet{}
	for _, group := range compactEquivalenceSet.Groups {
		for _, version := range compactEquivalenceSet.Versions {
			for _, kind := range compactEquivalenceSet.Kinds {
				equivalenceSet[schema.GroupVersionKind{Group: group, Version: version, Kind: kind}] = struct{}{}
			}
		}
	}
	return equivalenceSet
}

// Convert
func ExpandCompactRequirements(compactSyncRequirements CompactSyncRequirements) (SyncRequirements, error) {
	syncRequirements := SyncRequirements{}
	for _, compactRequirement := range compactSyncRequirements {
		requirement := GVKEquivalenceSet{}
		for _, compactEquivalenceSet := range compactRequirement {
			for equivalentGVK := range ExpandCompactEquivalenceSet(compactEquivalenceSet) {
				requirement[equivalentGVK] = struct{}{}
			}
		}
		syncRequirements = append(syncRequirements, requirement)
	}
	return syncRequirements, nil
}
