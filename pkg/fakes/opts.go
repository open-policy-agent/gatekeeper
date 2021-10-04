package fakes

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Name is the default name given to fake objects.
	Name = "foo"

	// Namespace is the default namespace given to fake namespace-scoped objects.
	Namespace = "bar"

	// UID is the default UID given to fake objects.
	UID = "abcd"
)

var (
	// defaultOpts are automatically applied to all fake objects to ensure they
	// are reasonably usable out-of-the-box.
	defaultOpts = []Opt{
		// Nearly all functionality which operates on client.Objects requires that
		// metadata.name be set.
		WithName(Name),

		// Setting an object as an OwnerReference fails validation if the referenced
		// object is missing a UID. This causes confusing test behavior, so we set
		// this by default.
		WithUID(UID),
	}

	// defaultNamespacedOpts are applied to fake objects of namespace-scoped Kinds.
	defaultNamespacedOpts = append(defaultOpts,
		// Some of our functionality relies on namespace-scoped objects always
		// having a defined Namespace. While in a cluster Kubernetes will correctly
		// set metadata.namespace if it is undefined, our logic is not robust to
		// fake namespace-scoped objects without metadata.namespace.
		WithNamespace(Namespace),
	)
)

// Opt modifies a client.Object during object instantiation.
// Generally, if there are conflicting opts (such as two WithName calls), the
// latter should win. This allows objects to have defaults which can easily be
// overridden.
type Opt func(client.Object)

// WithName sets the metadata.name of the object.
func WithName(name string) Opt {
	return func(object client.Object) {
		object.SetName(name)
	}
}

// WithNamespace sets the metadata.namespace of the object.
func WithNamespace(namespace string) Opt {
	return func(object client.Object) {
		object.SetNamespace(namespace)
	}
}

// WithUID sets the metadata.uid of the object.
func WithUID(uid types.UID) Opt {
	return func(object client.Object) {
		object.SetUID(uid)
	}
}

// WithLabels sets the metadata.labels of the object.
// Overwrites any existing labels on the object.
func WithLabels(labels map[string]string) Opt {
	return func(object client.Object) {
		object.SetLabels(labels)
	}
}
