package types

import (
	"encoding/json"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ID represent the identifier of a mutation object.
type ID struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

// Mutator represent a mutation object.
type Mutator interface {
	// Matches tells if the given object is eligible for this mutation.
	Matches(obj runtime.Object, ns *corev1.Namespace) bool
	// Mutate applies the mutation to the given object
	Mutate(obj *unstructured.Unstructured) (bool, error)
	// ID returns the id of the current mutator.
	ID() ID
	// Has diff tells if the mutator has meaningful differences
	// with the provided mutator
	HasDiff(mutator Mutator) bool
	// DeepCopy returns a copy of the current object
	DeepCopy() Mutator
	Value() (interface{}, error)
	Path() *parser.Path
	String() string
}

// MakeID builds an ID object for the given object
func MakeID(obj runtime.Object) (ID, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return ID{}, errors.Wrapf(err, "Failed to get accessor for %s %s", obj.GetObjectKind().GroupVersionKind().Group, obj.GetObjectKind().GroupVersionKind().Kind)
	}
	return ID{
		Group:     obj.GetObjectKind().GroupVersionKind().Group,
		Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
		Name:      meta.GetName(),
		Namespace: meta.GetNamespace(),
	}, nil
}

// UnmarshalValue unmarshals the value a mutation is meant to assign
func UnmarshalValue(data []byte) (interface{}, error) {
	value := make(map[string]interface{})
	err := json.Unmarshal(data, &value)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to unmarshal value %s", data)
	}
	return value["value"], nil
}
