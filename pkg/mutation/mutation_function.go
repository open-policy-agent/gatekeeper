package mutation

import (
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func Mutate(mutator Mutator, obj *unstructured.Unstructured) error {
	return mutate(mutator, obj.Object, nil, 0)
}

func mutate(m Mutator, current interface{}, previous interface{}, depth int) error {
	if len(m.Path().Nodes)-1 == depth {
		return addValue(m, current, previous, depth)
	}
	pathEntry := m.Path().Nodes[depth]
	switch castPathEntry := pathEntry.(type) {
	case *parser.Object:
		currentAsObject, ok := current.(map[string]interface{})
		if !ok { // Path entry type does not match current object
			return fmt.Errorf("mismatch between path entry (type: object) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		next, ok := currentAsObject[castPathEntry.Reference]
		if !ok { // Next element is missing and needs to be added
			next = createMissingElement(m, currentAsObject, previous, depth)
		}
		if err := mutate(m, next, current, depth+1); err != nil {
			return err
		}
		return nil
	case *parser.List:
		elementFound := false
		currentAsList, ok := current.([]interface{})
		if !ok { // Path entry type does not match current object
			return fmt.Errorf("mismatch between path entry (type: List) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		glob := castPathEntry.Glob
		key := castPathEntry.KeyField
		for _, listElement := range currentAsList {
			if glob {
				if err := mutate(m, listElement, current, depth+1); err != nil {
					return err
				}
				elementFound = true
			} else {
				if listElementAsObject, ok := listElement.(map[string]interface{}); ok {
					if elementValue, ok := listElementAsObject[key]; ok {
						if *castPathEntry.KeyValue == elementValue {
							if err := mutate(m, listElement, current, depth+1); err != nil {
								return err
							}
							elementFound = true
						}
					}
				}
			}
		}
		// If no matching element in the array was found in non Globbed list, create a new element
		if !castPathEntry.Glob && !elementFound {
			next := createMissingElement(m, current, previous, depth)
			if err := mutate(m, next, current, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid type pathEntry type: %T", pathEntry)
	}
	return nil
}

func addValue(m Mutator, current interface{}, previous interface{}, depth int) error {
	pathEntry := m.Path().Nodes[depth]
	switch castPathEntry := pathEntry.(type) {
	case *parser.Object:
		return setObjectValue(m, castPathEntry, current, depth)
	case *parser.List:
		return setListElementToValue(m, current, previous, castPathEntry, depth)
	}
	return nil
}

func setObjectValue(m Mutator, pathEntry *parser.Object, current interface{}, depth int) error {
	key := pathEntry.Reference
	switch m.(type) {
	case *AssignMetadataMutator:
		if elementValue, found, err := nestedString(current, key); err != nil {
			return err
		} else if found {
			log.Info("Mutated value already present", "field", key, "value", elementValue)
			return nil
		}
	}
	value, err := m.Value()
	if err != nil {
		return err
	}
	if err = setNestedField(current, value, key); err != nil {
		log.Error(err, "Failed to mutate object", "field", key, "value", value)
		return err
	}
	return nil
}

func setListElementToValue(m Mutator, current interface{}, previous interface{}, listPathEntry *parser.List, depth int) error {
	currentAsList, ok := current.([]interface{})
	if !ok {
		return fmt.Errorf("mismatch between path entry (type: list) and received object (type: %T). Path: %+v", current, listPathEntry)
	}
	if listPathEntry.Glob {
		return fmt.Errorf("last path entry can not be globbed")
	}
	newValue, err := m.Value()
	if err != nil {
		log.Error(err, "error getting mutator value for mutator %+v", m)
		return err
	}
	newValueAsObject, ok := newValue.(map[string]interface{})
	if !ok {
		return fmt.Errorf("last path entry of type list requires an object value, pathEntry: %+v", listPathEntry)
	}

	key := listPathEntry.KeyField
	keyValue := *listPathEntry.KeyValue

	elementFound := false
	for i, listElement := range currentAsList {
		if elementValue, found, err := nestedString(listElement, key); err != nil {
			return err
		} else if found && keyValue == elementValue {
			newKeyValue, ok := newValueAsObject[key]
			if !ok || newKeyValue != keyValue {
				return fmt.Errorf("key value of replaced object must not change")
			}
			elementFound = true
			currentAsList[i] = newValueAsObject
		}
	}
	if !elementFound {
		return appendNewObjectToList(m, currentAsList, previous, newValueAsObject, listPathEntry, depth)
	}
	return nil
}

func appendNewObjectToList(m Mutator, currentAsList []interface{}, previous interface{}, newValueAsObject map[string]interface{}, listPathEntry *parser.List, depth int) error {
	currentAsList = append(currentAsList, newValueAsObject)
	previousPath, ok := m.Path().Nodes[depth-1].(*parser.Object)
	if !ok {
		return fmt.Errorf("two consecutive list path entries not allowed: %+v %+v", listPathEntry, m.Path().Nodes[depth-1])
	}
	previousAsMap, ok := previous.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unable to handle nested arrays in mutated resources: %+v %+v", previous, currentAsList)
	}
	previousAsMap[previousPath.Reference] = currentAsList
	return nil
}

func createMissingElement(m Mutator, current interface{}, previous interface{}, depth int) interface{} {
	var next interface{}
	pathEntry := m.Path().Nodes[depth]
	nextPathEntry := m.Path().Nodes[depth+1]

	// Create new element of type
	switch nextPathEntry.(type) {
	case *parser.Object:
		next = make(map[string]interface{})
	case *parser.List:
		next = make([]interface{}, 0)
	}

	// Append to element of type
	switch castPathEntry := pathEntry.(type) {
	case *parser.Object:
		currentAsObject, ok := current.(map[string]interface{})
		if !ok { // Path entry type does not match current object
			return fmt.Errorf("mismatch between path entry (type: object) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		currentAsObject[castPathEntry.Reference] = next
	case *parser.List:
		currentAsList, ok := current.([]interface{})
		if !ok { // Path entry type does not match current object
			return fmt.Errorf("mismatch between path entry (type: List) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		current = append(currentAsList, next)
		nextAsObject, ok := next.(map[string]interface{})
		if !ok { // Path entry type does not match current object
			return fmt.Errorf("two consecutive list path entries not allowed: %+v %+v", castPathEntry, nextPathEntry)
		}
		nextAsObject[castPathEntry.KeyField] = *castPathEntry.KeyValue
		previousPathAsObject, ok := m.Path().Nodes[depth-1].(*parser.Object)
		if !ok {
			return fmt.Errorf("two consecutive list path entries not allowed: %+v %+v", castPathEntry, m.Path().Nodes[depth-1])
		}
		previousAsObject, ok := previous.(map[string]interface{})
		if !ok {
			return fmt.Errorf("two consecutive list objects not allowed: %+v %+v", current, previous)
		}
		previousAsObject[previousPathAsObject.Reference] = current
	}
	return next
}

func nestedString(current interface{}, key string) (string, bool, error) {
	currentAsMap, ok := current.(map[string]interface{})
	if !ok {
		return "", false, fmt.Errorf("cast error, unable to case %T to map[string]interface{}", current)
	}
	return unstructured.NestedString(currentAsMap, key)

}

func setNestedField(current interface{}, value interface{}, key string) error {
	currentAsMap, ok := current.(map[string]interface{})
	if !ok {
		return fmt.Errorf("cast error, unable to case %T to map[string]interface{}", current)
	}
	return unstructured.SetNestedField(currentAsMap, value, key)
}
