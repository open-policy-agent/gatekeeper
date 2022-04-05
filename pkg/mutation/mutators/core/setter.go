package core

import (
	"fmt"

	"github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
)

// Setter tells the mutate function what to do once we have found the
// node that needs mutating.
type Setter interface {
	// SetValue takes the object that needs mutating and the key of the
	// field on that object that should be mutated. It is up to the
	// implementor to actually mutate the object.
	SetValue(obj map[string]interface{}, key string) error

	// KeyedListOkay returns whether this setter can handle keyed lists.
	// If it can't, an attempt to mutate a keyed-list-type field will
	// result in an error.
	KeyedListOkay() bool

	// KeyedListValue is the value that will be assigned to the
	// targeted keyed list entry. Unlike SetValue(), this does
	// not do mutation directly.
	KeyedListValue() (map[string]interface{}, error)
}

var _ Setter = &defaultSetter{}

func NewDefaultSetter(value interface{}) Setter {
	return &defaultSetter{
		value: value,
	}
}

// defaultSetter is the default implementation of the Setter interface that supports
// assigning plain values and external data placeholders.
type defaultSetter struct {
	value interface{}
}

func (s *defaultSetter) KeyedListOkay() bool { return true }

func (s *defaultSetter) KeyedListValue() (map[string]interface{}, error) {
	valueAsObject, ok := s.value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("assign.value for keyed list is not an object: %+v", s.value)
	}
	return valueAsObject, nil
}

func (s *defaultSetter) SetValue(obj map[string]interface{}, key string) error {
	value := s.value
	incomingPlaceholder, isIncomingPlaceholder := value.(*unversioned.ExternalDataPlaceholder)
	if isIncomingPlaceholder {
		// make a copy of the incoming placeholder so we can modify it
		incomingPlaceholder = incomingPlaceholder.DeepCopy()
	}

	if _, ok := obj[key]; ok && isIncomingPlaceholder {
		switch prev := obj[key].(type) {
		case *unversioned.ExternalDataPlaceholder:
			incomingPlaceholder.ValueAtLocation = prev.ValueAtLocation
		case string:
			incomingPlaceholder.ValueAtLocation = prev
		default:
			return fmt.Errorf("value assigned to external data placeholder is not a string, got %v (%T)", value, value)
		}
	}

	if incomingPlaceholder != nil {
		value = incomingPlaceholder
	}

	obj[key] = value
	return nil
}
