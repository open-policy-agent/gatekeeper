package expansion

import (
	"fmt"

	expansionunversioned "github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type cycleDetector interface {
	addTemplate(*expansionunversioned.ExpansionTemplate) error
	removeTemplate(*expansionunversioned.ExpansionTemplate)
}

// gvkGraph represents a directed acyclic graph of GVKs. The terminal int value
// represents how many edges exists between two GVKs.
type gvkGraph map[schema.GroupVersionKind]map[schema.GroupVersionKind]int

// addTemplate tries to upsert the template to the graph, returning an error if
// a cycle is created. No edges are added if a cycle is encountered.
func (g gvkGraph) addTemplate(template *expansionunversioned.ExpansionTemplate) error {
	y := genGVKToSchemaGVK(template.Spec.GeneratedGVK)
	xs := applyToGVKs(template)
	for _, x := range xs {
		if g.checkCycle(x, y) {
			return fmt.Errorf("template forms expansion cycle starting with %v to %v", x, y)
		}
	}

	for _, x := range xs {
		g.addEdge(x, y)
	}
	return nil
}

func (g gvkGraph) removeTemplate(template *expansionunversioned.ExpansionTemplate) {
	y := genGVKToSchemaGVK(template.Spec.GeneratedGVK)
	xs := applyToGVKs(template)
	for _, x := range xs {
		g.removeEdge(x, y)
	}
}

func (g gvkGraph) addEdge(x, y schema.GroupVersionKind) {
	if _, exists := g[x]; !exists {
		g[x] = make(map[schema.GroupVersionKind]int)
	}
	g[x][y]++
}

func (g gvkGraph) removeEdge(x, y schema.GroupVersionKind) {
	if _, exists := g[x]; !exists {
		return
	}
	if _, exists := g[x][y]; !exists {
		return
	}

	g[x][y]--
	// Prune empty edges
	if g[x][y] == 0 {
		delete(g[x], y)
		if len(g[x]) == 0 {
			delete(g, x)
		}
	}
}

// checkCycle returns true if adding an edge from `x` to `y` creates a cycle.
func (g gvkGraph) checkCycle(x, y schema.GroupVersionKind) bool {
	// If adding x->y creates a cycle, then that cycle must contain x->y
	// Traverse the graph starting at y, and make sure we don't end up back at x
	current := map[schema.GroupVersionKind]bool{y: true}
	for len(current) > 0 {
		for c := range current {
			if c == x {
				return true
			}
			delete(current, c)
			for _, n := range g.getNeighbors(c) {
				current[n] = true
			}
		}
	}

	return false
}

func (g gvkGraph) getNeighbors(gvk schema.GroupVersionKind) []schema.GroupVersionKind {
	if _, exists := g[gvk]; !exists {
		return nil
	}

	neighbors := make([]schema.GroupVersionKind, len(g[gvk]))
	i := 0
	for x := range g[gvk] {
		neighbors[i] = x
		i++
	}
	return neighbors
}

// applyToGVKs returns all GVKs specified in a template's `applyTo`.
func applyToGVKs(template *expansionunversioned.ExpansionTemplate) []schema.GroupVersionKind {
	var gvks []schema.GroupVersionKind
	for _, apply := range template.Spec.ApplyTo {
		for _, g := range apply.Groups {
			for _, v := range apply.Versions {
				for _, k := range apply.Kinds {
					gvks = append(gvks, schema.GroupVersionKind{Group: g, Version: v, Kind: k})
				}
			}
		}
	}
	return gvks
}
