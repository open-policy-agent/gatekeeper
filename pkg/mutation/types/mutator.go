package types

import (
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ID represent the identifier of a mutation object.
type ID struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

func (id ID) String() string {
	return fmt.Sprintf("%v %v",
		schema.GroupKind{Group: id.Group, Kind: id.Kind},
		client.ObjectKey{Namespace: id.Namespace, Name: id.Name})
}

// Mutator represent a mutation object.
type Mutator interface {
	// Matches tells if the given object is eligible for this mutation.
	Matches(mutable *Mutable) bool
	// Mutate applies the mutation to the given object
	Mutate(mutable *Mutable) (bool, error)
	// UsesExternalData returns true if the mutation uses external data.
	UsesExternalData() bool
	// ID returns the id of the current mutator.
	ID() ID
	// HasDiff tells if the mutator has meaningful differences
	// with the provided mutator
	HasDiff(mutator Mutator) bool
	// DeepCopy returns a copy of the current object
	DeepCopy() Mutator
	Path() parser.Path
	String() string
}

// MetadataGetter is an object that can retrieve
// the metadata fields that support `AssignField.FromMetadata`.
type MetadataGetter interface {
	GetNamespace() string
	GetName() string
}

// MakeID builds an ID object for the given object.
func MakeID(obj client.Object) ID {
	return ID{
		Group:     obj.GetObjectKind().GroupVersionKind().Group,
		Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
}

// UnmarshalValue unmarshals the value a mutation is meant to assign.
func UnmarshalValue(data []byte) (interface{}, error) {
	value := make(map[string]interface{})
	err := json.Unmarshal(data, &value)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to unmarshal value %s", data)
	}
	return value["value"], nil
}
