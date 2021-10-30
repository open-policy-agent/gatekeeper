package mutation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSystem_Mutate_Fail(t *testing.T) {
	m := &errorMutator{err: errors.New("some error")}

	s := NewSystem(SystemOpts{})

	err := s.Upsert(m)
	if err != nil {
		t.Fatal(err)
	}

	u := &unstructured.Unstructured{}
	gotMutated, gotErr := s.Mutate(u, nil)

	if gotMutated != false {
		t.Errorf("got Mutate() = %t, want %t", gotMutated, false)
	}

	if gotErr == nil {
		t.Errorf("got Mutate() error = %v, want error", gotErr)
	}
}

type errorMutator struct {
	err error
}

var _ types.Mutator = &errorMutator{}

func (e errorMutator) Matches(client.Object, *corev1.Namespace) bool {
	return true
}

func (e errorMutator) Mutate(*unstructured.Unstructured) (bool, error) {
	return false, e.err
}

func (e errorMutator) ID() types.ID {
	return types.ID{}
}

func (e errorMutator) HasDiff(types.Mutator) bool {
	panic("implement me")
}

func (e errorMutator) DeepCopy() types.Mutator {
	return errorMutator{err: fmt.Errorf(e.err.Error())}
}

func (e errorMutator) Value(_ types.MetadataGetter) (interface{}, error) {
	return nil, nil
}

func (e errorMutator) Path() parser.Path {
	return parser.Path{}
}

func (e errorMutator) String() string {
	return ""
}
