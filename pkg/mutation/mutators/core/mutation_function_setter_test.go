package core

import (
	"errors"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type notKeyedSetter struct{}

func (s *notKeyedSetter) KeyedListOkay() bool { return false }

func (s *notKeyedSetter) KeyedListValue() (map[string]interface{}, error) {
	panic("notKeyedSetter setter does not handle keyed lists")
}

func (s *notKeyedSetter) SetValue(_ map[string]interface{}, _ string) error {
	panic("NOT IMPLEMENTED")
}

func TestKeyedListIncompatible(t *testing.T) {
	path, err := parser.Parse(`spec.containers[name: "foo"]`)
	if err != nil {
		t.Fatal(err)
	}
	obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
	m, err := Mutate(path, &tester.Tester{}, &notKeyedSetter{}, obj)
	if !errors.Is(err, ErrNonKeyedSetter) {
		t.Errorf("wanted err = %+v, got %+v", ErrNonKeyedSetter, err)
	}
	if m != false {
		t.Error("expected m=false, got true")
	}
}
