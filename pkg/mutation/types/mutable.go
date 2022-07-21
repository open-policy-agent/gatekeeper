package types

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Mutable represents a mutable object and its metadata.
type Mutable struct {
	// Object is the object to be mutated.
	Object *unstructured.Unstructured

	// Namespace is the namespace of the mutable object.
	Namespace *corev1.Namespace

	// Username is the name of the user who initiates
	// admission request of the mutable object.
	Username string

	// Source specifies which types of resources the mutator should be applied to
	Source SourceType
}
