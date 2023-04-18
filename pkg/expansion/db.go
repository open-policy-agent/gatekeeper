package expansion

import (
	"errors"
	"fmt"

	"github.com/dominikbraun/graph"
	expansionunversioned "github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type templateDB interface {
	upsert(*expansionunversioned.ExpansionTemplate) error
	remove(*expansionunversioned.ExpansionTemplate)
	templatesForGVK(gvk schema.GroupVersionKind) []*expansionunversioned.ExpansionTemplate
	getConflicts() IDSet
}

var _ templateDB = &db{}

type adjList map[schema.GroupVersionKind]IDSet

// hashID is passed to the graphing package.
var hashID = func(id TemplateID) string {
	return string(id)
}

type templateState struct {
	template     *expansionunversioned.ExpansionTemplate
	hasConflicts bool
}

type edge struct {
	x TemplateID
	y TemplateID
}

type db struct {
	store map[TemplateID]*templateState

	// graph stores a graph of ExpansionTemplate. A directed edge from template A
	// to B means that the template A's `generatedGVK` matches template B's `applyTo`.
	graph graph.Graph[string, TemplateID]

	// `matchers` and `generators` creates the necessary mappings to be able to
	// determine the inbound and outbound edges of a given template in O(1).
	// matchers is a mapping of GVKs to templates that match (applyTo) that GVK.
	matchers adjList
	// generators is a mapping of GVKs to templates that generate that GVK.
	generators adjList
}

func newDB() *db {
	return &db{
		store:      make(map[TemplateID]*templateState),
		graph:      graph.New(hashID, graph.Directed()),
		matchers:   make(adjList),
		generators: make(adjList),
	}
}

func (d *db) getConflicts() IDSet {
	confs := make(IDSet)
	for tID, tState := range d.store {
		if tState.hasConflicts {
			confs[tID] = true
		}
	}
	return confs
}

// handleAdd adds the template to the DB, returning true if a cycle was created.
// The template is added even if a cycle was found.
func (d *db) handleAdd(template *expansionunversioned.ExpansionTemplate) (bool, error) {
	id := keyForTemplate(template)

	// We should always remove the old template before handleAdd. If we
	// didn't, that's a bug.
	if _, exists := d.store[id]; exists {
		panic(fmt.Errorf("tried to add template %q that already exists", id))
	}

	// Update storage
	d.store[id] = &templateState{template: template.DeepCopy(), hasConflicts: false}

	// Update generators
	genGVK := genGVKToSchemaGVK(template.Spec.GeneratedGVK)
	if _, exists := d.generators[genGVK]; !exists {
		d.generators[genGVK] = make(map[TemplateID]bool)
	}
	d.generators[genGVK][id] = true

	// Update matchers
	matches := applyToGVKs(template)
	for _, m := range matches {
		if _, exists := d.matchers[m]; !exists {
			d.matchers[m] = make(map[TemplateID]bool)
		}
		d.matchers[m][id] = true
	}

	// Add vertex if DNE
	if _, err := d.graph.Vertex(hashID(id)); err != nil {
		if errors.Is(err, graph.ErrVertexNotFound) {
			if err := d.graph.AddVertex(id); err != nil {
				return false, fmt.Errorf("adding vertex to graph: %w", err)
			}
		} else {
			return false, fmt.Errorf("unexpected error getting vertex for template %s: %w", id, err)
		}
	}

	// Add edges
	edges := d.edgesForTemplate(template)
	cycle := false
	for _, e := range edges {
		from := hashID(e.x)
		to := hashID(e.y)

		createsCycle, err := graph.CreatesCycle(d.graph, from, to)
		if err != nil {
			return false, fmt.Errorf("checking cycle for template %s: %w", id, err)
		}
		cycle = createsCycle || cycle

		if err = d.graph.AddEdge(from, to); err != nil {
			return false, fmt.Errorf("adding edge for template %s: %w", id, err)
		}
	}

	return cycle, nil
}

func (d *db) edgesForTemplate(template *expansionunversioned.ExpansionTemplate) []edge {
	var edges []edge
	id := keyForTemplate(template)
	genGVK := genGVKToSchemaGVK(template.Spec.GeneratedGVK)

	// Add out-bound edges (from this template's generated GVK to other
	// templates' matched GVKs)
	for t := range d.matchers[genGVK] {
		edges = append(edges, edge{id, t})
	}

	// Add in-bound edges (from other templates' generated GVK to this template's
	// matched GVK)
	for _, gen := range applyToGVKs(template) {
		for t := range d.generators[gen] {
			edges = append(edges, edge{t, id})
		}
	}

	return edges
}

func (d *db) handleRemove(template *expansionunversioned.ExpansionTemplate) {
	id := keyForTemplate(template)

	// The template must exist. Existence checks should be done upstream.
	if _, exists := d.store[id]; !exists {
		panic(fmt.Errorf("called handleRemove for template %q, but template DNE in store", id))
	}

	// Update storage
	delete(d.store, id)

	// Update generators
	genGVK := genGVKToSchemaGVK(template.Spec.GeneratedGVK)
	if _, exists := d.generators[genGVK]; exists {
		delete(d.generators[genGVK], id)
		if len(d.generators[genGVK]) == 0 {
			delete(d.generators, genGVK)
		}
	} else {
		panic(fmt.Errorf("[template %q] inconsistent db - expected key %s to exist in generators", id, genGVK))
	}

	// Update matchers
	matches := applyToGVKs(template)
	for _, m := range matches {
		if _, exists := d.matchers[m]; exists {
			delete(d.matchers[m], id)
			if len(d.matchers[m]) == 0 {
				delete(d.matchers, m)
			}
		} else {
			panic(fmt.Errorf("[template %q] inconsistent db - expected key %s to exist in matches", id, m))
		}
	}

	// Remove edges
	edges := d.edgesForTemplate(template)
	for _, e := range edges {
		from := hashID(e.x)
		to := hashID(e.y)
		if err := d.graph.RemoveEdge(from, to); err != nil {
			panic(fmt.Errorf("[template %q] unexpected error removing edge: %w", id, err))
		}
	}
}

func (d *db) updateCycles() {
	// First reset all conflicts
	for _, ts := range d.store {
		ts.hasConflicts = false
	}

	conflicts, err := graph.StronglyConnectedComponents(d.graph)
	if err != nil {
		panic(fmt.Errorf("error getting SCCs: %w", err))
	}
	// All strongly connect components containing more than 1 vertex are a cycle
	for _, scc := range conflicts {
		if len(scc) <= 1 {
			continue
		}
		for _, id := range scc {
			d.store[TemplateID(id)].hasConflicts = true
		}
	}
}

func (d *db) upsert(template *expansionunversioned.ExpansionTemplate) error {
	id := keyForTemplate(template)
	old, hasOld := d.store[id]
	if hasOld {
		d.handleRemove(old.template)
	}

	newCycle, err := d.handleAdd(template)
	if err != nil {
		return fmt.Errorf("adding template to db: %w", err)
	}
	// If the new/updated template caused a cycle, or the previous template belonged
	// to a cycle, then we need to re-check the graph for cycles
	if newCycle || (hasOld && old.hasConflicts) {
		d.updateCycles()
	}

	if newCycle {
		return fmt.Errorf("template forms expansion cycle")
	}
	return nil
}

func (d *db) remove(template *expansionunversioned.ExpansionTemplate) {
	id := keyForTemplate(template)
	old, exists := d.store[id]
	if !exists {
		return
	}

	d.handleRemove(old.template)
	// If the removed template was part of a cycle, we need to recheck the graph
	// in case that cycle was resolved
	if old.hasConflicts {
		d.updateCycles()
	}
}

func (d *db) templatesForGVK(gvk schema.GroupVersionKind) []*expansionunversioned.ExpansionTemplate {
	var tset []*expansionunversioned.ExpansionTemplate
	for tID := range d.matchers[gvk] {
		// Sanity check. In theory, this should never happen, but if it does, we
		// can't recover.
		if _, exists := d.store[tID]; !exists {
			panic(fmt.Errorf("inconsistent db - key %s exists in matchers but not cache", tID))
		}

		tState := d.store[tID]
		if !tState.hasConflicts {
			tset = append(tset, tState.template)
		}
	}

	return tset
}

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
