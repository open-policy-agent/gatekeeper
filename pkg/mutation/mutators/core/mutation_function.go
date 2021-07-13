package core

import (
	"errors"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	path "github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("mutation").WithValues(logging.Process, "mutation")

func Mutate(mutator types.Mutator, tester *path.Tester, valueTest func(interface{}, bool) bool, obj *unstructured.Unstructured) (bool, error) {
	s := &mutatorState{mutator: mutator, tester: tester, valueTest: valueTest}
	if len(mutator.Path().Nodes) == 0 {
		return false, fmt.Errorf("mutator %v has an empty target location", mutator.ID())
	}
	if obj == nil {
		return false, errors.New("attempting to mutate a nil object")
	}
	mutated, _, err := s.mutateInternal(obj.Object, 0)
	return mutated, err
}

type mutatorState struct {
	mutator types.Mutator
	tester  *path.Tester
	// valueTest takes the input value and whether that value already existed.
	// It returns true if the value should be mutated
	valueTest func(interface{}, bool) bool
}

// mutateInternal mutates the resource recursively. It returns false if there has been no change
// to any downstream objects in the tree, indicating that the mutation should not be persisted.
func (s *mutatorState) mutateInternal(current interface{}, depth int) (bool, interface{}, error) {
	pathEntry := s.mutator.Path().Nodes[depth]
	switch castPathEntry := pathEntry.(type) {
	case *parser.Object:
		currentAsObject, ok := current.(map[string]interface{})
		if !ok { // Path entry type does not match current object
			return false, nil, fmt.Errorf("mismatch between path entry (type: object) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		next, exists := currentAsObject[castPathEntry.Reference]
		if exists {
			if !s.tester.ExistsOkay(depth) {
				return false, nil, nil
			}
		} else {
			if !s.tester.MissingOkay(depth) {
				return false, nil, nil
			}
		}
		// we have hit the end of our path, this is the base case
		if len(s.mutator.Path().Nodes)-1 == depth {
			if s.valueTest != nil && !s.valueTest(next, exists) {
				return false, nil, nil
			}
			value, err := s.mutator.Value()
			if err != nil {
				return false, nil, err
			}
			currentAsObject[castPathEntry.Reference] = value
			return true, currentAsObject, nil
		}
		if !exists { // Next element is missing and needs to be added
			var err error
			next, err = s.createMissingElement(depth)
			if err != nil {
				return false, nil, err
			}
		}
		mutated, next, err := s.mutateInternal(next, depth+1)
		if err != nil {
			return false, nil, err
		}
		if mutated {
			currentAsObject[castPathEntry.Reference] = next
		}
		return mutated, currentAsObject, nil
	case *parser.List:
		elementFound := false
		currentAsList, ok := current.([]interface{})
		if !ok { // Path entry type does not match current object
			return false, nil, fmt.Errorf("mismatch between path entry (type: List) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		shallowCopy := make([]interface{}, len(currentAsList))
		copy(shallowCopy, currentAsList)
		if len(s.mutator.Path().Nodes)-1 == depth {
			return s.setListElementToValue(shallowCopy, castPathEntry, depth)
		}

		glob := castPathEntry.Glob
		key := castPathEntry.KeyField
		// if someone says "MustNotExist" for a glob, that condition can never be satisfied
		if glob && !s.tester.ExistsOkay(depth) {
			return false, nil, nil
		}
		mutated := false
		for _, listElement := range shallowCopy {
			if glob {
				m, _, err := s.mutateInternal(listElement, depth+1)
				if err != nil {
					return false, nil, err
				}
				mutated = mutated || m
				elementFound = true
			} else if listElementAsObject, ok := listElement.(map[string]interface{}); ok {
				if elementValue, ok := listElementAsObject[key]; ok {
					if castPathEntry.KeyValue == elementValue {
						if !s.tester.ExistsOkay(depth) {
							return false, nil, nil
						}
						m, _, err := s.mutateInternal(listElement, depth+1)
						if err != nil {
							return false, nil, err
						}
						mutated = mutated || m
						elementFound = true
					}
				}
			}
		}
		// If no matching element in the array was found in non Globbed list, create a new element
		if !castPathEntry.Glob && !elementFound {
			if !s.tester.MissingOkay(depth) {
				return false, nil, nil
			}
			next, err := s.createMissingElement(depth)
			if err != nil {
				return false, nil, err
			}
			shallowCopy = append(shallowCopy, next)
			m, _, err := s.mutateInternal(next, depth+1)
			if err != nil {
				return false, nil, err
			}
			mutated = mutated || m
		}
		return mutated, shallowCopy, nil
	default:
		return false, nil, fmt.Errorf("invalid type pathEntry type: %T", pathEntry)
	}
}

func (s *mutatorState) setListElementToValue(currentAsList []interface{}, listPathEntry *parser.List, depth int) (bool, []interface{}, error) {
	if listPathEntry.Glob {
		return false, nil, fmt.Errorf("last path entry can not be globbed")
	}
	newValue, err := s.mutator.Value()
	if err != nil {
		log.Error(err, "error getting mutator value for mutator %+v", s.mutator)
		return false, nil, err
	}
	newValueAsObject, ok := newValue.(map[string]interface{})
	if !ok {
		return false, nil, fmt.Errorf("last path entry of type list requires an object value, pathEntry: %+v", listPathEntry)
	}

	key := listPathEntry.KeyField
	if listPathEntry.KeyValue == nil {
		return false, nil, errors.New("encountered nil key value when setting a new list element")
	}
	keyValue := listPathEntry.KeyValue

	for i, listElement := range currentAsList {
		if elementValue, found, err := nestedFieldNoCopy(listElement, key); err != nil {
			return false, nil, err
		} else if found && keyValue == elementValue {
			newKeyValue, ok := newValueAsObject[key]
			if !ok || newKeyValue != keyValue {
				return false, nil, fmt.Errorf("key value of replaced object must not change")
			}
			if !s.tester.ExistsOkay(depth) {
				return false, nil, nil
			}
			currentAsList[i] = newValueAsObject
			return true, currentAsList, nil
		}
	}
	if !s.tester.MissingOkay(depth) {
		return false, nil, nil
	}
	return true, append(currentAsList, newValueAsObject), nil
}

func (s *mutatorState) createMissingElement(depth int) (interface{}, error) {
	var next interface{}
	pathEntry := s.mutator.Path().Nodes[depth]
	nextPathEntry := s.mutator.Path().Nodes[depth+1]

	// Create new element of type
	switch nextPathEntry.(type) {
	case *parser.Object:
		next = make(map[string]interface{})
	case *parser.List:
		next = make([]interface{}, 0)
	}

	// Set new keyfield
	if castPathEntry, ok := pathEntry.(*parser.List); ok {
		nextAsObject, ok := next.(map[string]interface{})
		if !ok { // Path entry type does not match current object
			return nil, fmt.Errorf("two consecutive list path entries not allowed: %+v %+v", castPathEntry, nextPathEntry)
		}
		if castPathEntry.KeyValue == nil {
			return nil, fmt.Errorf("list entry has no key value")
		}
		nextAsObject[castPathEntry.KeyField] = castPathEntry.KeyValue
	}
	return next, nil
}

func nestedFieldNoCopy(current interface{}, key string) (interface{}, bool, error) {
	currentAsMap, ok := current.(map[string]interface{})
	if !ok {
		return "", false, fmt.Errorf("cast error, unable to case %T to map[string]interface{}", current)
	}
	return unstructured.NestedFieldNoCopy(currentAsMap, key)
}
