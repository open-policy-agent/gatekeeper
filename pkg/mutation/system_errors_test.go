package mutation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSystem_Mutate_Fail(t *testing.T) {
	m := &errorMutator{err: errors.New("some error")}

	s := NewSystem(SystemOpts{})

	err := s.Upsert(m)
	if err != nil {
		t.Fatal(err)
	}

	u := &unstructured.Unstructured{}
	gotMutated, gotErr := s.Mutate(&types.Mutable{Object: u})

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

func (e errorMutator) Matches(*types.Mutable) bool {
	return true
}

func (e errorMutator) Mutate(*types.Mutable) (bool, error) {
	return false, e.err
}

func (e errorMutator) UsesExternalData() bool {
	return false
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
