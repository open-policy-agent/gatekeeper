package expansion

import (
	mutationtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Expandable represents an expandable object and its metadata.
type Expandable struct {
	// Object is the object to be expanded.
	Object *unstructured.Unstructured

	// OldObject is the old version of the object (if applicable) to be expanded.
	OldObject *unstructured.Unstructured

	// Namespace is the namespace of the expandable object.
	Namespace *corev1.Namespace

	// Username is the name of the user who initiates
	// admission request of the expandable object.
	Username string

	// Source specifies which types of resources the mutator should be applied to
	Source mutationtypes.SourceType
}

type Resultant struct {
	Obj               *unstructured.Unstructured
	OldObj            *unstructured.Unstructured
	TemplateName      string
	EnforcementAction string
}
