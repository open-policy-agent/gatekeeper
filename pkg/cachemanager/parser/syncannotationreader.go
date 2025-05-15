package parser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// syncAnnotationName is the name of the annotation that stores
// GVKS that are required to be synced.
const SyncAnnotationName = "metadata.gatekeeper.sh/requires-sync-data"

// SyncRequirements contains a list of ANDed requirements, each of which
// contains a GVK equivalence set.
type SyncRequirements []GVKEquivalenceSet

// GVKEquivalenceSet is a set of GVKs that a template can use
// interchangeably in its referential policy implementation.
type GVKEquivalenceSet map[schema.GroupVersionKind]struct{}

// CompactSyncRequirements contains a list of ANDed requirements, each of
// which contains a list of GVK clauses.
type CompactSyncRequirements [][]GVKClause

// GVKClause contains a set of equivalent GVKs, expressed
// in the form [groups, versions, kinds] where any combination of
// items from these three fields can be considered a valid option.
// Used for unmarshalling as this is the form used in requiressync annotations.
type GVKClause struct {
	Groups   []string `json:"groups"`
	Versions []string `json:"versions"`
	Kinds    []string `json:"kinds"`
}

// ReadSyncRequirements parses the sync requirements from a
// constraint template.
func ReadSyncRequirements(t *templates.ConstraintTemplate) (SyncRequirements, error) {
	if t.Annotations != nil {
		if annotation, exists := t.Annotations[SyncAnnotationName]; exists {
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

// Takes a GVK Clause and expands it into a GVKEquivalenceSet (to be unioned
// with the GVKEquivalenceSet expansions of the other clauses).
func ExpandGVKClause(clause GVKClause) GVKEquivalenceSet {
	equivalenceSet := GVKEquivalenceSet{}
	for _, group := range clause.Groups {
		for _, version := range clause.Versions {
			for _, kind := range clause.Kinds {
				equivalenceSet[schema.GroupVersionKind{Group: group, Version: version, Kind: kind}] = struct{}{}
			}
		}
	}
	return equivalenceSet
}

// Takes a CompactSyncRequirements (the json form provided in the template
// annotation) and expands it into a SyncRequirements.
func ExpandCompactRequirements(compactSyncRequirements CompactSyncRequirements) (SyncRequirements, error) {
	syncRequirements := SyncRequirements{}
	for _, compactRequirement := range compactSyncRequirements {
		requirement := GVKEquivalenceSet{}
		for _, clause := range compactRequirement {
			for equivalentGVK := range ExpandGVKClause(clause) {
				requirement[equivalentGVK] = struct{}{}
			}
		}
		syncRequirements = append(syncRequirements, requirement)
	}
	return syncRequirements, nil
}

func (s GVKEquivalenceSet) String() string {
	var sb strings.Builder
	for gvk := range s {
		if sb.Len() != 0 {
			sb.WriteString(" OR ")
		}
		sb.WriteString(fmt.Sprintf("%s/%s:%s", gvk.Group, gvk.Version, gvk.Kind))
	}
	return sb.String()
}

func (s SyncRequirements) String() string {
	var sb strings.Builder
	for _, equivSet := range s {
		if sb.Len() != 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("- %v", equivSet))
	}
	return sb.String()
}
