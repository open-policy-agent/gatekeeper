package schema

import (
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
)

// node is an element of an implicit schema.
// Allows for the definition of overlapping schemas. See Add.
type node struct {
	// ReferencedBy tracks the Mutations which reference this part of the schema tree.
	ReferencedBy IDSet

	// Children is the set of child Nodes a this location in the schema.
	// Each node defines a distinct child definition. If multiple Nodes are defined
	// for the same child, then there is a schema conflict.
	Children map[string]map[parser.NodeType]node

	// MustTerminate tracks which mutators require their path to terminate, i.e.
	// the path must not be extended by any other mutation.
	MustTerminate IDSet
}

// Add inserts the provided path, linked to the given ID.
//
// Returns the set of conflicts detected while adding the path. Conflicts occur
// when elements with the same path have different types, for example:
//
// spec.containers[name: foo].image
// spec.containers.image
//
// If mustTerminate is true, any future paths that extend beyond this path will
// result in a conflict, for example:
//
// spec.foo <- must terminate at foo
// spec.foo.bar <- conflict since foo is a terminal node
//
// If the returned references is non-nil it contains at least two elements, one
// of which is the passed id.
func (n *node) Add(id types.ID, path []parser.Node, terminalType parser.NodeType, mustTerminate bool) IDSet {
	if n.ReferencedBy == nil {
		n.ReferencedBy = make(IDSet)
	}
	if n.MustTerminate == nil {
		n.MustTerminate = make(IDSet)
	}

	// This node is referenced by the passed ID.
	n.ReferencedBy[id] = true

	// Base case; there is no more path to validate.
	if len(path) == 0 {
		if mustTerminate {
			n.MustTerminate[id] = true
		}
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
	childType := headType(path, terminalType)
	if _, exists := n.Children[childKey][childType]; !exists {
		n.Children[childKey][childType] = node{}
	}

	// Add the remaining path to the appropriate child, collecting any conflicts
	// found when adding it.
	child := n.Children[childKey][childType]
	conflicts := child.Add(id, path[1:], terminalType, mustTerminate)
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
func (n *node) Remove(id types.ID, path []parser.Node, terminalType parser.NodeType, mustTerminate bool) {
	// This ID no longer references this node.
	if _, isReferenced := n.ReferencedBy[id]; isReferenced {
		delete(n.ReferencedBy, id)
	} else {
		panic(ErrNotFound)
	}

	if len(path) == 0 {
		// No more path to remove.
		if _, found := n.MustTerminate[id]; found && mustTerminate {
			delete(n.MustTerminate, id)
		} else if mustTerminate {
			panic(fmt.Errorf("%s not found in MustTerminate IDSet", id))
		}
		return
	}

	childKey := key(path[0])
	if _, found := n.Children[childKey]; !found {
		// The child does not exist.
		panic(fmt.Errorf("no child for key %q: %w", childKey, ErrNotFound))
	}
	childType := headType(path, terminalType)
	if _, found := n.Children[childKey][childType]; !found {
		// No child of the key and type exists.
		// This is how we detect that the path for id is incomplete. If the path
		// were complete, the type of the child was known when Add()ed but not when
		// Remove()d and is files as unknown.
		panic(fmt.Errorf("no child of type %q for key %q: %w", childType, childKey, ErrNotFound))
	}

	child := n.Children[childKey][childType]
	child.Remove(id, path[1:], terminalType, mustTerminate)

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

func (n *node) conflicts(childKey string) IDSet {
	conflicts := make(IDSet)

	// Count the number of distinct types with this key.
	nTypes := len(n.Children[childKey])
	if _, hasUnknown := n.Children[childKey][Unknown]; hasUnknown {
		// Nodes whose types we are unable to determine do not count against this
		// check.
		nTypes--
	}

	mustTerminate := false
	hasGrandchildren := false
	for nodeType, child := range n.Children[childKey] {
		mustTerminate = mustTerminate || len(child.MustTerminate) > 0
		hasGrandchildren = hasGrandchildren || len(child.Children) > 0
		if nodeType == Unknown {
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

	// we can return here if one of the following is true:
	// 1) we don't have to terminate at this node
	// 2) we have to terminate at this node but we don't have grandchildren for this child key
	if !mustTerminate || !hasGrandchildren {
		return conflicts
	}

	// since we have to terminate at this node,
	// the terminal node and its grandchildren are considered conflicts
	for _, child := range n.Children[childKey] {
		conflicts = merge(conflicts, child.MustTerminate)
		for _, nodeMap := range child.Children {
			for _, grandchild := range nodeMap {
				conflicts = merge(conflicts, grandchild.ReferencedBy)
			}
		}
	}

	// If more than 1 non-unknown types are declared, this node is part of a
	// schema conflict.
	return conflicts
}

// GetConflicts returns all conflicts along the passed path.
func (n *node) GetConflicts(path []parser.Node, terminalType parser.NodeType) []types.ID {
	conflictsMap := n.getConflicts(path, terminalType)
	return conflictsMap.ToList()
}

func (n *node) getConflicts(path []parser.Node, terminalType parser.NodeType) IDSet {
	if len(path) == 0 {
		return nil
	}

	childKey := key(path[0])
	childType := headType(path, terminalType)
	if _, found := n.Children[childKey]; !found {
		// Path has not been added, so there can be no conflicts.
		return nil
	}

	if _, found := n.Children[childKey][childType]; !found {
		// Path has not been added, so there can be no conflicts.
		return nil
	}
	child := n.Children[childKey][childType]

	childConflicts := child.getConflicts(path[1:], terminalType)

	// Count the number of distinct types with this key.
	conflictsMap := n.conflicts(childKey)

	if childConflicts == nil {
		childConflicts = make(IDSet)
	}
	for conflict := range childConflicts {
		conflictsMap[conflict] = true
	}

	return conflictsMap
}

// merge inserts elements from `from` into `into`. Returns `into`, or a
// reference to a new map if `into` is nil.
func merge(into, from IDSet) IDSet {
	if len(into) == 0 && len(from) == 0 {
		return nil
	}
	if into == nil {
		into = make(IDSet)
	}
	for k := range from {
		into[k] = true
	}
	return into
}

// headType returns the type of the second Node, if it exists.
// This is essential for determining whether the current location in a schema
// path is a list.
func headType(path []parser.Node, terminalType parser.NodeType) parser.NodeType {
	if len(path) < 2 {
		// Default to the terminal type, as we are at the last path node.
		return terminalType
	}
	return path[1].Type()
}

func (n *node) DeepCopy() *node {
	if n == nil {
		return nil
	}

	result := &node{
		ReferencedBy:  make(IDSet),
		Children:      make(map[string]map[parser.NodeType]node),
		MustTerminate: n.MustTerminate,
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
