package mutation

import (
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
)

// ID represent the identifier of a mutation object.
type ID struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

// SchemaBinding represent the specific GVKs that a
// mutation's implicit schema applies to
type SchemaBinding struct {
	Groups   []string
	Kinds    []string
	Versions []string
}

// Mutator represent a mutation object.
type Mutator interface {
	// Matches tells if the given object is eligible for this mutation.
	Matches(obj metav1.Object, ns *corev1.Namespace) bool
	// Mutate applies the mutation to the given object
	Mutate(obj *unstructured.Unstructured) error
	// ID returns the id of the current mutator.
	ID() ID
	// Has diff tells if the mutator has meaningful differences
	// with the provided mutator
	HasDiff(mutator Mutator) bool
}

// MutatorWithSchema is a mutator exposing the implied
// schema of the target object.
type MutatorWithSchema interface {
	Mutator
	SchemaBindings() []SchemaBinding
	Path() parser.Path
}

// MakeID builds an ID object for the given object
func MakeID(obj runtime.Object) (ID, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return ID{}, errors.Wrapf(err, "Failed to get accessor for %s - %s", obj.GetObjectKind().GroupVersionKind().Group, obj.GetObjectKind().GroupVersionKind().Kind)

	}
	return ID{
		Group:     obj.GetObjectKind().GroupVersionKind().Group,
		Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
		Name:      meta.GetName(),
		Namespace: meta.GetNamespace(),
	}, nil
}
