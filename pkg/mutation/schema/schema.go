package schema

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Binding represent the specific GVKs that a
// mutation's implicit schema applies to
type Binding struct {
	Groups   []string
	Kinds    []string
	Versions []string
}

// MutatorWithSchema is a mutator exposing the implied
// schema of the target object.
type MutatorWithSchema interface {
	types.Mutator
	SchemaBindings() []Binding
}

var (
	log = logf.Log.WithName("mutation_schema")
)

func getSortedGVKs(bindings []Binding) []schema.GroupVersionKind {
	// deduplicate GVKs
	gvksMap := map[schema.GroupVersionKind]struct{}{}
	for _, binding := range bindings {
		for _, group := range binding.Groups {
			for _, version := range binding.Versions {
				for _, kind := range binding.Kinds {
					gvk := schema.GroupVersionKind{
						Group:   group,
						Version: version,
						Kind:    kind,
					}
					gvksMap[gvk] = struct{}{}
				}
			}
		}
	}

	gvks := []schema.GroupVersionKind{}
	for gvk := range gvksMap {
		gvks = append(gvks, gvk)
	}

	// we iterate over the map in a stable order so that
	// unit tests wont be flaky
	sort.Slice(gvks, func(i, j int) bool { return gvks[i].String() < gvks[j].String() })

	return gvks
}

// New returns a new schema database
func New() *DB {
	return &DB{
		mutators: map[types.ID]MutatorWithSchema{},
		schemas:  map[schema.GroupVersionKind]*scheme{},
	}
}

// DB is a database that caches all the implied schemas.
// Will return an error when adding a mutator conflicting with the existing ones.
type DB struct {
	mutex    sync.Mutex
	mutators map[types.ID]MutatorWithSchema
	schemas  map[schema.GroupVersionKind]*scheme
}

// Upsert tries to insert or update the given mutator.
// If a conflict is detected, Upsert will return an error
func (db *DB) Upsert(mutator MutatorWithSchema) error {
	db.mutex.Lock()
	defer db.mutex.Unlock()
	return db.upsert(mutator, true)
}

func (db *DB) upsert(mutator MutatorWithSchema, unwind bool) error {
	oldMutator, ok := db.mutators[mutator.ID()]
	// unwind will be set to false if we are actually unwinding a bad commit
	if ok && !oldMutator.HasDiff(mutator) && unwind {
		return nil
	}
	if ok && unwind {
		db.remove(oldMutator.ID())
	}

	modified := []*scheme{}
	for _, gvk := range getSortedGVKs(mutator.SchemaBindings()) {
		s, ok := db.schemas[gvk]
		if !ok {
			s = &scheme{gvk: gvk}
			db.schemas[gvk] = s
		}
		if err := s.add(mutator.Path().Nodes); err != nil {
			// avoid infinite recursion
			if unwind {
				db.unwind(mutator, oldMutator, modified)
			}
			return err
		}
		modified = append(modified, s)
	}
	m := mutator.DeepCopy()
	db.mutators[mutator.ID()] = m.(MutatorWithSchema)
	return nil
}

// unwind a bad commit
func (db *DB) unwind(new, old MutatorWithSchema, schemes []*scheme) {
	for _, s := range schemes {
		s.remove(new.Path().Nodes)
		if s.root == nil {
			delete(db.schemas, s.gvk)
		}
	}
	if old == nil {
		return
	}
	if err := db.upsert(old, false); err != nil {
		// We removed all changes made by the previous mutator and
		// are re-adding a mutator that was already present. Because
		// this mutator was already present and we have a lock on the
		// db, this should never fail. If it does we are in an unknown
		// state and should panic so we can recover by bootstrapping
		// and raise the visibility of the issue.
		log.Error(err, "could not upsert previously existing mutator into schema, this is not recoverable")
		panic(err)
	}
}

// Remove removes the mutator with the given id from the
// db.
func (db *DB) Remove(id types.ID) {
	db.mutex.Lock()
	defer db.mutex.Unlock()
	db.remove(id)
	delete(db.mutators, id)
}

func (db *DB) remove(id types.ID) {
	mutator, ok := db.mutators[id]
	if !ok {
		// no mutator found, nothing to do
		return
	}

	for _, gvk := range getSortedGVKs(mutator.SchemaBindings()) {
		s, ok := db.schemas[gvk]
		if !ok {
			log.Error(nil, "mutator associated with missing schema", "mutator", id, "schema", gvk)
			panic(fmt.Sprintf("mutator %v associated with missing schema %v", id, gvk))
		}
		s.remove(mutator.Path().Nodes)
		if s.root == nil {
			delete(db.schemas, gvk)
		}
	}
}

type scheme struct {
	gvk  schema.GroupVersionKind
	root *node
}

func (s *scheme) add(ref []parser.Node) error {
	if s.root == nil {
		s.root = &node{}
	}
	return s.root.add(ref)
}

func (s *scheme) remove(ref []parser.Node) {
	if s.root == nil {
		return
	}
	s.root.remove(ref)
	if s.root.referenceCount == 0 {
		s.root = nil
	}
}

type node struct {
	referenceCount uint
	nodeType       parser.NodeType

	// list-type nodes have a key field and only one child
	keyField *string
	child    *node

	// object-type nodes only have children
	children map[string]*node
}

// backup creates a shallow copy of the node we can restore in case of error
func (n *node) backup() *node {
	return &node{
		referenceCount: n.referenceCount,
		nodeType:       n.nodeType,
		keyField:       n.keyField,
	}
}

func (n *node) restore(backup *node) {
	n.referenceCount = backup.referenceCount
	n.nodeType = backup.nodeType
	n.keyField = backup.keyField
}

func (n *node) add(ref []parser.Node) error {
	backup := n.backup()
	n.referenceCount++

	// we should stop recursing when len(ref) == 1, but
	// this guards against infinite recursion in the case
	// where there is a bug in the switch code below
	if len(ref) == 0 {
		return nil
	}

	err := func() error {
		current := ref[0]
		switch t := current.Type(); t {
		case parser.ObjectNode:
			if n.nodeType != "" && n.nodeType != parser.ObjectNode {
				return fmt.Errorf("node type conflict: %v vs %v", n.nodeType, parser.ObjectNode)
			}
			n.nodeType = parser.ObjectNode
			obj := current.(*parser.Object)
			if n.children == nil {
				n.children = make(map[string]*node)
			}
			// we stop recursing down the path right before the last element
			// because the last element has no more type information to add
			if len(ref) == 1 {
				return nil
			}
			child, ok := n.children[obj.Reference]
			if !ok {
				child = &node{}
			}
			if err := child.add(ref[1:]); err != nil {
				return wrapObjErr(obj, err)
			}
			n.children[obj.Reference] = child
		case parser.ListNode:
			if n.nodeType != "" && n.nodeType != parser.ListNode {
				return fmt.Errorf("node type conflict: %v vs %v", n.nodeType, parser.ListNode)
			}
			n.nodeType = parser.ListNode
			list := current.(*parser.List)
			if n.keyField != nil && *n.keyField != list.KeyField {
				return fmt.Errorf("key field conflict: %s vs %s", *n.keyField, list.KeyField)
			}
			if n.keyField == nil {
				n.keyField = &list.KeyField
			}
			// we stop recursing down the path right before the last element
			// because the last element has no more type information to add
			if len(ref) == 1 {
				return nil
			}
			child := n.child
			if child == nil {
				child = &node{}
			}
			if err := child.add(ref[1:]); err != nil {
				return wrapListErr(list, err)
			}
			n.child = child
		default:
			return fmt.Errorf("unknown node type: %v", t)
		}
		return nil
	}()

	if err != nil {
		n.restore(backup)
	}

	return err
}

func (n *node) remove(ref []parser.Node) {
	n.referenceCount--
	// we don't add any schema nodes when the length
	// of `ref` == 1, so we shouldn't remove any
	// either
	if len(ref) == 1 {
		return
	}
	current := ref[0]
	switch t := current.Type(); t {
	case parser.ObjectNode:
		obj := current.(*parser.Object)
		if n.children == nil {
			// no children means nothing to clean
			return
		}
		child, ok := n.children[obj.Reference]
		if !ok {
			// child is missing, nothing to clean
			return
		}
		// decrementing the reference count would remove the
		// object, we can stop traversing the tree
		if child.referenceCount <= 1 {
			delete(n.children, obj.Reference)
			return
		}
		child.remove(ref[1:])
		return
	case parser.ListNode:
		if n.child == nil {
			// no child, nothing to clean
			return
		}
		// decrementing the reference count would remove the
		// object, we can stop traversing the tree
		if n.child.referenceCount <= 1 {
			n.child = nil
			return
		}
		n.child.remove(ref[1:])
		return
	default:
		log.Error(fmt.Errorf("unknown node type"), "unknown node type", "node_type", t)
		panic(fmt.Sprintf("unknown node type, schema db in unknown state: %s", t))
	}
}

var _ error = &Error{}

// Error holds errors processing the schema
type Error struct {
	nodeName   string
	childError error
}

func (e *Error) Error() string {
	builder := &strings.Builder{}
	current := e
	for {
		builder.WriteString(current.nodeName)
		child, ok := current.childError.(*Error)
		if !ok {
			break
		}
		current = child
	}
	builder.WriteString(": ")
	builder.WriteString(current.childError.Error())
	return strings.TrimPrefix(builder.String(), ".")
}

func wrapObjErr(obj *parser.Object, err error) *Error {
	return &Error{
		childError: err,
		nodeName:   fmt.Sprintf(".%s", obj.Reference),
	}
}

func wrapListErr(list *parser.List, err error) *Error {
	var value string
	if list.Glob {
		value = "*"
	} else {
		value = fmt.Sprintf("\"%s\"", *list.KeyValue)
	}
	return &Error{
		childError: err,
		nodeName:   fmt.Sprintf("[\"%s\": %s]", list.KeyField, value),
	}
}
