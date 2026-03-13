package process

import (
	"fmt"
	"reflect"
	"sync"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

// excluderEntry is a processed form of a MatchEntry, scoped to a single process.
type excluderEntry struct {
	excludedNamespaces []wildcard.Wildcard
	apiGroups          map[string]bool       // nil means match all groups
	apiVersions        map[string]bool       // nil means match all versions
	kinds              map[string]bool       // nil means match all kinds
	namespaceSelector  *metav1.LabelSelector // original selector spec, used for equality comparison
	compiledSelector   labels.Selector       // pre-compiled selector for fast matching
}

type Excluder struct {
	mux     sync.RWMutex
	entries map[Process][]excluderEntry
}

var allProcesses = []Process{
	Audit,
	Webhook,
	Mutation,
	Sync,
}

var processExcluder = &Excluder{
	entries: make(map[Process][]excluderEntry),
}

func Get() *Excluder {
	return processExcluder
}

func New() *Excluder {
	return &Excluder{
		entries: make(map[Process][]excluderEntry),
	}
}

func (s *Excluder) Add(entry []configv1alpha1.MatchEntry) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	for _, matchEntry := range entry {
		e := excluderEntry{
			excludedNamespaces: matchEntry.ExcludedNamespaces,
			namespaceSelector:  matchEntry.NamespaceSelector,
		}

		if matchEntry.NamespaceSelector != nil {
			compiled, err := metav1.LabelSelectorAsSelector(matchEntry.NamespaceSelector)
			if err != nil {
				return fmt.Errorf("invalid namespaceSelector: %w", err)
			}
			e.compiledSelector = compiled
		}

		if len(matchEntry.APIGroups) > 0 {
			e.apiGroups = make(map[string]bool, len(matchEntry.APIGroups))
			for _, g := range matchEntry.APIGroups {
				e.apiGroups[g] = true
			}
		}
		if len(matchEntry.APIVersions) > 0 {
			e.apiVersions = make(map[string]bool, len(matchEntry.APIVersions))
			for _, v := range matchEntry.APIVersions {
				e.apiVersions[v] = true
			}
		}
		if len(matchEntry.Kinds) > 0 {
			e.kinds = make(map[string]bool, len(matchEntry.Kinds))
			for _, k := range matchEntry.Kinds {
				e.kinds[k] = true
			}
		}

		for _, op := range matchEntry.Processes {
			// adding entry to all processes for "*"
			if Process(op) == Star {
				for _, p := range allProcesses {
					s.entries[p] = append(s.entries[p], e)
				}
			} else {
				s.entries[Process(op)] = append(s.entries[Process(op)], e)
			}
		}
	}

	return nil
}

func (s *Excluder) Replace(new *Excluder) { // nolint:revive
	s.mux.Lock()
	defer s.mux.Unlock()
	s.entries = new.entries
}

func (s *Excluder) Equals(new *Excluder) bool { // nolint:revive
	s.mux.RLock()
	defer s.mux.RUnlock()
	return entriesMapEqual(s.entries, new.entries)
}

// EqualsForProcess checks if the entries for a specific process are equal.
func (s *Excluder) EqualsForProcess(process Process, new *Excluder) bool { // nolint:revive
	s.mux.RLock()
	defer s.mux.RUnlock()
	return entriesEqual(s.entries[process], new.entries[process])
}

// entriesMapEqual compares two process-to-entries maps, ignoring compiled selectors.
func entriesMapEqual(a, b map[Process][]excluderEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for p, ae := range a {
		be, ok := b[p]
		if !ok {
			return false
		}
		if !entriesEqual(ae, be) {
			return false
		}
	}
	return true
}

// entriesEqual compares two entry slices, ignoring compiled selectors.
func entriesEqual(a, b []excluderEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i].excludedNamespaces, b[i].excludedNamespaces) {
			return false
		}
		if !reflect.DeepEqual(a[i].apiGroups, b[i].apiGroups) {
			return false
		}
		if !reflect.DeepEqual(a[i].apiVersions, b[i].apiVersions) {
			return false
		}
		if !reflect.DeepEqual(a[i].kinds, b[i].kinds) {
			return false
		}
		if !reflect.DeepEqual(a[i].namespaceSelector, b[i].namespaceSelector) {
			return false
		}
	}
	return true
}

// IsNamespaceExcluded checks if an object should be excluded based on its namespace (or name for
// Namespace objects), GVK, and namespace labels (for Namespace objects only).
// For full namespace selector support on non-Namespace objects, use IsObjectExcluded.
func (s *Excluder) IsNamespaceExcluded(process Process, obj client.Object) (bool, error) {
	isNamespace := obj.GetObjectKind().GroupVersionKind().Kind == "Namespace" &&
		obj.GetObjectKind().GroupVersionKind().Group == ""

	var nsLabels map[string]string
	if isNamespace {
		nsLabels = obj.GetLabels()
	}

	return s.IsObjectExcluded(process, obj, nsLabels)
}

// IsObjectExcluded checks if an object should be excluded based on all configured criteria:
// namespace name patterns, GVK, and namespace label selectors.
// nsLabels should be the labels of the namespace containing the object (or the object's own
// labels if it is a Namespace).
func (s *Excluder) IsObjectExcluded(process Process, obj client.Object, nsLabels map[string]string) (bool, error) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	for i := range s.entries[process] {
		if s.entries[process][i].matches(obj, nsLabels) {
			return true, nil
		}
	}

	return false, nil
}

// GetExcludedNamespaces returns a deduplicated list of excluded namespace patterns for the given
// process. Only returns patterns from entries that apply to all resource types (no GVK filter)
// and don't use a namespace label selector.
func (s *Excluder) GetExcludedNamespaces(process Process) []string {
	s.mux.RLock()
	defer s.mux.RUnlock()

	seen := make(map[string]struct{})
	var excludedNamespaces []string
	for _, e := range s.entries[process] {
		if e.apiGroups != nil || e.apiVersions != nil || e.kinds != nil || e.namespaceSelector != nil {
			continue
		}
		for _, ns := range e.excludedNamespaces {
			nsStr := string(ns)
			if _, exists := seen[nsStr]; exists {
				continue
			}
			seen[nsStr] = struct{}{}
			excludedNamespaces = append(excludedNamespaces, nsStr)
		}
	}

	return excludedNamespaces
}

// matches checks whether an object matches this entry's criteria.
// All specified criteria (namespace patterns, GVK, namespace selector) must match (AND logic).
// Unspecified criteria (nil/empty) match everything.
func (e *excluderEntry) matches(obj client.Object, nsLabels map[string]string) bool {
	gvk := obj.GetObjectKind().GroupVersionKind()

	// Check GVK filters
	if e.apiGroups != nil && !e.apiGroups[gvk.Group] {
		return false
	}
	if e.apiVersions != nil && !e.apiVersions[gvk.Version] {
		return false
	}
	if e.kinds != nil && !e.kinds[gvk.Kind] {
		return false
	}

	// Determine namespace to check
	isNamespace := gvk.Kind == "Namespace" && gvk.Group == ""
	namespace := obj.GetNamespace()
	if isNamespace {
		namespace = obj.GetName()
	}

	// Check namespace name patterns
	if len(e.excludedNamespaces) > 0 {
		if !wildcardMatch(e.excludedNamespaces, namespace) {
			return false
		}
	}

	// Check namespace label selector (pre-compiled during Add)
	if e.compiledSelector != nil {
		if nsLabels == nil {
			// No namespace labels available; can't evaluate selector, so don't match
			return false
		}

		if !e.compiledSelector.Matches(labels.Set(nsLabels)) {
			return false
		}
	}

	return true
}

func wildcardMatch(patterns []wildcard.Wildcard, ns string) bool {
	for _, p := range patterns {
		if p.Matches(ns) {
			return true
		}
	}
	return false
}
