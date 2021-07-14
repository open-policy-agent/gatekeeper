package schema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
)

type idSet map[types.ID]bool

// unknown represents a path element we do not know the type of.
// Elements of type unknown do not conflict with path elements of known types.
const unknown = parser.NodeType("Unknown")

func (c idSet) String() string {
	var keys []string
	for k := range c {
		keys = append(keys, fmt.Sprintf("%q", k.String()))
	}
	sort.Strings(keys)
	return fmt.Sprintf("[%s]", strings.Join(keys, ","))
}

// node is an element of an implicit schema.
// Allows for the definition of overlapping schemas. See Add.
type node struct {
	// ReferencedBy tracks the Mutations which reference this part of the schema tree.
	ReferencedBy idSet

	// Children is the set of child Nodes a this location in the schema.
	// Each node defines a distinct child definition. If multiple Nodes are defined
	// for the same child, then there is a schema conflict.
	Children map[string]map[parser.NodeType]node
}

// Add inserts the provided path, linked to the given ID.
//
// Returns the set of conflicts detected while adding the path. Conflicts occur
// when elements with the same path have different types, for example:
//
// spec.containers[name: foo].image
// spec.containers.image
//
// If the returned references is non-nil it contains at least two elements, one
// of which is the passed id.
func (n *node) Add(id types.ID, path []parser.Node) idSet {
	if n.ReferencedBy == nil {
		n.ReferencedBy = make(map[types.ID]bool)
	}
	// This node is referenced by the passed ID.
	n.ReferencedBy[id] = true

	// Base case; there is no more path to validate.
	if len(path) == 0 {
		return nil
	}

	// Initialize child within n.children.
	childNode := path[0]
	if n.Children == nil {
		n.Children = make(map[string]map[parser.NodeType]node)
	}
	childKey := key(childNode)
	if n.Children[childKey] == nil {
		n.Children[childKey] = make(map[parser.NodeType]node)
	}
	childType := headType(path)
	if _, exists := n.Children[childKey][childType]; !exists {
		n.Children[childKey][childType] = node{}
	}

	// Add the remaining path to the appropriate child, collecting any conflicts
	// found when adding it.
	child := n.Children[childKey][childType]
	conflicts := child.Add(id, path[1:])
	n.Children[childKey][childType] = child

	// Detect conflicts at this node.
	// We know there is a conflict if there is a child with the same Key but a
	// different type.
	conflicts = merge(conflicts, n.conflicts(childKey))
	return conflicts
}

const ErrNotFound = util.Error("path not found")

// Remove removes the id and path from the tree.
// Panics if the ID is not defined or was Add()ed with a different path.
func (n *node) Remove(id types.ID, path []parser.Node) {
	// This ID no longer references this node.
	if _, isReferenced := n.ReferencedBy[id]; isReferenced {
		delete(n.ReferencedBy, id)
	} else {
		panic(ErrNotFound)
	}

	if len(path) == 0 {
		// No more path to remove.
		return
	}

	childKey := key(path[0])
	if _, found := n.Children[childKey]; !found {
		// The child does not exist.
		panic(fmt.Errorf("no child for key %q: %w", childKey, ErrNotFound))
	}
	childType := headType(path)
	if _, found := n.Children[childKey][childType]; !found {
		// No child of the key and type exists.
		// This is how we detect that the path for id is incomplete. If the path
		// were complete, the type of the child was known when Add()ed but not when
		// Remove()d and is files as unknown.
		panic(fmt.Errorf("no child of type %q for key %q: %w", childType, childKey, ErrNotFound))
	}

	child := n.Children[childKey][childType]
	child.Remove(id, path[1:])

	// Delete the type from the child if it is no longer referenced.
	if len(child.ReferencedBy) == 0 {
		// No references to this child of this type exist.
		delete(n.Children[childKey], childType)
	} else {
		n.Children[childKey][childType] = child
	}

	// Delete the child if it is no longer referenced.
	if len(n.Children[childKey]) == 0 {
		delete(n.Children, childKey)
	}
}

func (n *node) conflicts(childKey string) idSet {
	conflicts := make(idSet)

	// Count the number of distinct types with this key.
	nTypes := len(n.Children[childKey])
	if _, hasUnknown := n.Children[childKey][unknown]; hasUnknown {
		// Nodes whose types we are unable to determine do not count against this
		// check.
		nTypes--
	}

	for nodeType, child := range n.Children[childKey] {
		if nodeType == unknown {
			// If we don't know the type of a node, we assume it conflicts with nothing.
			continue
		}
		// There are conflicts if either:
		// 1) there are more than one non-unknown types for the Child, or
		// 2) the Child is a List and defines multiple keys.
		if nTypes > 1 || nodeType == parser.ListNode && len(child.Children) > 1 {
			conflicts = merge(conflicts, child.ReferencedBy)
		}
	}

	// If more than 1 non-unknown types are declared, this node is part of a
	// schema conflict.
	return conflicts
}

// HasConflicts returns true if there are any schema type conflicts along the
// passed path.
//
// Returns an error if the path does not exist.
func (n *node) HasConflicts(path []parser.Node) (bool, error) {
	if len(path) == 0 {
		return false, nil
	}

	childKey := key(path[0])
	childType := headType(path)
	if _, found := n.Children[childKey]; !found {
		// Path has not been added, so there can be no conflicts.
		return false, ErrNotFound
	}

	// Count the number of distinct types with this key.
	if len(n.conflicts(childKey)) > 0 {
		return true, nil
	}

	if _, found := n.Children[childKey][childType]; !found {
		// Path has not been added, so there can be no conflicts.
		return false, ErrNotFound
	}
	child := n.Children[childKey][childType]
	return child.HasConflicts(path[1:])
}

// merge inserts elements from `from` into `into`. Returns `into`, or a
// reference to a new map if `into` is nil.
func merge(into, from idSet) idSet {
	if len(into) == 0 && len(from) == 0 {
		return nil
	}
	if into == nil {
		into = make(idSet)
	}
	for k := range from {
		into[k] = true
	}
	return into
}

// headType returns the type of the second Node, if it exists.
// This is essential for determining whether the current location in a schema
// path is a list.
func headType(path []parser.Node) parser.NodeType {
	if len(path) < 2 {
		// Default unknown as we have no information either way.
		return unknown
	}
	return path[1].Type()
}

func (n *node) DeepCopy() *node {
	if n == nil {
		return nil
	}

	result := &node{
		ReferencedBy: make(idSet),
		Children:     make(map[string]map[parser.NodeType]node),
	}
	for id := range n.ReferencedBy {
		result.ReferencedBy[id] = true
	}
	for k, ts := range n.Children {
		newChildren := make(map[parser.NodeType]node)
		for t, child := range ts {
			newChildren[t] = *child.DeepCopy()
		}
		result.Children[k] = newChildren
	}
	return result
}

// key extracts the unique identifier of the next element in the path from the
// given Node for use in the node tree.
func key(n parser.Node) string {
	switch t := n.(type) {
	case *parser.Object:
		return t.Reference
	case *parser.List:
		return t.KeyField
	default:
		panic(fmt.Sprintf("unknown node type %T", n))
	}
}
