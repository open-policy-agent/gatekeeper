package mutation

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

func TestSystem_Mutate_Annotations(t *testing.T) {
	MutationAnnotationsEnabled = ptr.To[bool](true)
	t.Cleanup(func() {
		MutationAnnotationsEnabled = ptr.To[bool](false)
	})

	m := &fakeMutator{
		MID:    types.ID{Name: "mutation-1"},
		Labels: map[string]string{"foo": "bar"},
	}
	mid := uuid.UUID{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8}
	s := NewSystem(SystemOpts{NewUUID: func() uuid.UUID {
		return mid
	}})

	err := s.Upsert(m)
	if err != nil {
		t.Fatal(err)
	}

	obj := &unstructured.Unstructured{}

	mutated, err := s.Mutate(context.Background(), &types.Mutable{Object: obj})
	if err != nil {
		t.Fatalf("got Mutate() error = %v, want %v", err, nil)
	}
	if !mutated {
		t.Fatalf("got Mutate() = %t, want %t", mutated, true)
	}

	want := &unstructured.Unstructured{}
	want.SetLabels(map[string]string{"foo": "bar"})
	want.SetAnnotations(map[string]string{
		annotationMutations:  toAnnotationMutationsValue([][]types.Mutator{{m}}),
		annotationMutationID: mid.String(),
	})

	if diff := cmp.Diff(want, obj); diff != "" {
		t.Error(diff)
	}
}
