package mutation

import (
	"github.com/google/go-cmp/cmp"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

//AssignMetadataMutator is a mutator built out of an
// AssignMeta instance.
type AssignMetadataMutator struct {
	id             ID
	assignMetadata *mutationsv1alpha1.AssignMetadata
}

// assignMetadataMutator implements mutator
var _ Mutator = &AssignMetadataMutator{}

func (m *AssignMetadataMutator) Matches(obj metav1.Object, ns *corev1.Namespace) bool {
	// TODO implement using matches function
	return false
}

func (m *AssignMetadataMutator) Mutate(obj *unstructured.Unstructured) error {
	// TODO implement
	return nil
}
func (m *AssignMetadataMutator) ID() ID {
	return m.id
}

func (m *AssignMetadataMutator) HasDiff(mutator Mutator) bool {
	toCheck, ok := mutator.(*AssignMetadataMutator)
	if !ok { // different types, different
		return true
	}

	if !cmp.Equal(toCheck.id, m.id) {
		return true
	}
	// any difference in spec may be enough
	if !cmp.Equal(toCheck.assignMetadata.Spec, m.assignMetadata.Spec) {
		return true
	}

	return false
}

func (m *AssignMetadataMutator) DeepCopy() Mutator {
	res := &AssignMetadataMutator{
		id:             m.id,
		assignMetadata: m.assignMetadata.DeepCopy(),
	}
	return res
}

// MutatorForAssignMetadata builds an AssignMetadataMutator from the given AssignMetadata object.
func MutatorForAssignMetadata(assignMeta *mutationsv1alpha1.AssignMetadata) (*AssignMetadataMutator, error) {
	id, err := MakeID(assignMeta)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to retrieve id for assignMetadata type")
	}
	return &AssignMetadataMutator{
		id:             id,
		assignMetadata: assignMeta.DeepCopy(),
	}, nil
}
