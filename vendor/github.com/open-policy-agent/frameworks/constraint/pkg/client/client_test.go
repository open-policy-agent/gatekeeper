package client

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"text/template"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1alpha1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestClientE2E(t *testing.T) {
	d := local.New()
	p, err := NewProbe(d)
	if err != nil {
		t.Fatal(err)
	}
	for name, f := range p.TestFuncs() {
		t.Run(name, func(t *testing.T) {
			if err := f(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

var _ TargetHandler = &badHandler{}

type badHandler struct {
	Name        string
	Errors      bool
	HasLib      bool
	HandlesData bool
}

func (h *badHandler) GetName() string {
	return h.Name
}

func (h *badHandler) Library() *template.Template {
	if !h.HasLib {
		return nil
	}
	return template.Must(template.New("foo").Parse(`
package foo
matching_constraints[c] {c = data.c}
matching_reviews_and_constraints[[r,c]] {r = data.r; c = data.c}`))
}

func (h *badHandler) MatchSchema() apiextensionsv1beta1.JSONSchemaProps {
	return apiextensionsv1beta1.JSONSchemaProps{}
}

func (h *badHandler) ProcessData(obj interface{}) (bool, string, interface{}, error) {
	if h.Errors {
		return false, "", nil, errors.New("TEST ERROR")
	}
	if !h.HandlesData {
		return false, "", nil, nil
	}
	return true, "projects/something", nil, nil
}

func (h *badHandler) HandleReview(obj interface{}) (bool, interface{}, error) {
	return false, "", nil
}

func (h *badHandler) HandleViolation(result *types.Result) error {
	return nil
}

func (h *badHandler) ValidateConstraint(u *unstructured.Unstructured) error {
	return nil
}

func TestInvalidTargetName(t *testing.T) {
	tc := []struct {
		Name          string
		Handler       TargetHandler
		ErrorExpected bool
	}{
		{
			Name:          "Acceptable Name",
			Handler:       &badHandler{Name: "Hello8", HasLib: true},
			ErrorExpected: false,
		},
		{
			Name:          "No Name",
			Handler:       &badHandler{Name: ""},
			ErrorExpected: true,
		},
		{
			Name:          "No Dots",
			Handler:       &badHandler{Name: "asdf.asdf"},
			ErrorExpected: true,
		},
		{
			Name:          "No Spaces",
			Handler:       &badHandler{Name: "asdf asdf"},
			ErrorExpected: true,
		},
		{
			Name:          "Must start with a letter",
			Handler:       &badHandler{Name: "8asdf"},
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			d := local.New()
			b, err := NewBackend(Driver(d))
			if err != nil {
				t.Fatalf("Could not create backend: %s", err)
			}
			_, err = b.NewClient(Targets(tt.Handler))
			if (err == nil) && tt.ErrorExpected {
				t.Fatalf("err = nil; want non-nil")
			}
			if (err != nil) && !tt.ErrorExpected {
				t.Fatalf("err = \"%s\"; want nil", err)
			}
		})
	}
}

func TestAddData(t *testing.T) {
	tc := []struct {
		Name      string
		Handler1  TargetHandler
		Handler2  TargetHandler
		ErroredBy []string
		HandledBy []string
	}{
		{
			Name:      "Handled By Both",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: true},
			HandledBy: []string{"h1", "h2"},
		},
		{
			Name:      "Handled By One",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: false},
			HandledBy: []string{"h1"},
		},
		{
			Name:      "Errored By One",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: true, Errors: true},
			HandledBy: []string{"h1"},
			ErroredBy: []string{"h2"},
		},
		{
			Name:      "Errored By Both",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true, Errors: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: true, Errors: true},
			ErroredBy: []string{"h1", "h2"},
		},
		{
			Name:     "Handled By None",
			Handler1: &badHandler{Name: "h1", HasLib: true, HandlesData: false},
			Handler2: &badHandler{Name: "h2", HasLib: true, HandlesData: false},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			d := local.New()
			b, err := NewBackend(Driver(d))
			if err != nil {
				t.Fatalf("Could not create backend: %s", err)
			}
			c, err := b.NewClient(Targets(tt.Handler1, tt.Handler2))
			if err != nil {
				t.Fatal(err)
			}
			r, err := c.AddData(context.Background(), nil)
			if err != nil && len(tt.ErroredBy) == 0 {
				t.Errorf("err = %s; want nil", err)
			}
			expectedErr := make(map[string]bool)
			actualErr := make(map[string]bool)
			for _, v := range tt.ErroredBy {
				expectedErr[v] = true
			}
			if e, ok := err.(ErrorMap); ok {
				for k, _ := range e {
					actualErr[k] = true
				}
			}
			if !reflect.DeepEqual(actualErr, expectedErr) {
				t.Errorf("errSet = %v; wanted %v", actualErr, expectedErr)
			}
			expectedHandled := make(map[string]bool)
			for _, v := range tt.HandledBy {
				expectedHandled[v] = true
			}
			if !reflect.DeepEqual(r.Handled, expectedHandled) {
				t.Errorf("handledSet = %v; wanted %v", r.Handled, expectedHandled)
			}
			if r.HandledCount() != len(expectedHandled) {
				t.Errorf("HandledCount() = %v; want %v", r.HandledCount(), len(expectedHandled))
			}
		})
	}
}

func TestRemoveData(t *testing.T) {
	tc := []struct {
		Name      string
		Handler1  TargetHandler
		Handler2  TargetHandler
		ErroredBy []string
		HandledBy []string
	}{
		{
			Name:      "Handled By Both",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: true},
			HandledBy: []string{"h1", "h2"},
		},
		{
			Name:      "Handled By One",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: false},
			HandledBy: []string{"h1"},
		},
		{
			Name:      "Errored By One",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: true, Errors: true},
			HandledBy: []string{"h1"},
			ErroredBy: []string{"h2"},
		},
		{
			Name:      "Errored By Both",
			Handler1:  &badHandler{Name: "h1", HasLib: true, HandlesData: true, Errors: true},
			Handler2:  &badHandler{Name: "h2", HasLib: true, HandlesData: true, Errors: true},
			ErroredBy: []string{"h1", "h2"},
		},
		{
			Name:     "Handled By None",
			Handler1: &badHandler{Name: "h1", HasLib: true, HandlesData: false},
			Handler2: &badHandler{Name: "h2", HasLib: true, HandlesData: false},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			d := local.New()
			b, err := NewBackend(Driver(d))
			if err != nil {
				t.Fatalf("Could not create backend: %s", err)
			}
			c, err := b.NewClient(Targets(tt.Handler1, tt.Handler2))
			if err != nil {
				t.Fatal(err)
			}
			r, err := c.RemoveData(context.Background(), nil)
			if err != nil && len(tt.ErroredBy) == 0 {
				t.Errorf("err = %s; want nil", err)
			}
			expectedErr := make(map[string]bool)
			actualErr := make(map[string]bool)
			for _, v := range tt.ErroredBy {
				expectedErr[v] = true
			}
			if e, ok := err.(ErrorMap); ok {
				for k, _ := range e {
					actualErr[k] = true
				}
			}
			if !reflect.DeepEqual(actualErr, expectedErr) {
				t.Errorf("errSet = %v; wanted %v", actualErr, expectedErr)
			}
			expectedHandled := make(map[string]bool)
			for _, v := range tt.HandledBy {
				expectedHandled[v] = true
			}
			if !reflect.DeepEqual(r.Handled, expectedHandled) {
				t.Errorf("handledSet = %v; wanted %v", r.Handled, expectedHandled)
			}
			if r.HandledCount() != len(expectedHandled) {
				t.Errorf("HandledCount() = %v; want %v", r.HandledCount(), len(expectedHandled))
			}
		})
	}
}

func TestAddTemplate(t *testing.T) {
	badRegoTempl := createTemplate(name("fakes"), crdNames("Fake", "fakes"), targets("h1"))
	badRegoTempl.Spec.Targets[0].Rego = "asd{"
	tc := []struct {
		Name          string
		Handler       TargetHandler
		Template      *v1alpha1.ConstraintTemplate
		ErrorExpected bool
	}{
		{
			Name:          "Good Template",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      createTemplate(name("fakes"), crdNames("Fake", "fakes"), targets("h1")),
			ErrorExpected: false,
		},
		{
			Name:          "Unknown Target",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      createTemplate(name("fakes"), crdNames("Fake", "fakes"), targets("h2")),
			ErrorExpected: true,
		},
		{
			Name:          "Bad CRD",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      createTemplate(name("fakes"), targets("h1")),
			ErrorExpected: true,
		},
		{
			Name:          "No Name",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      createTemplate(crdNames("Fake", "fakes"), targets("h1")),
			ErrorExpected: true,
		},
		{
			Name:          "Bad Rego",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      badRegoTempl,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			d := local.New()
			b, err := NewBackend(Driver(d))
			if err != nil {
				t.Fatalf("Could not create backend: %s", err)
			}
			c, err := b.NewClient(Targets(tt.Handler))
			if err != nil {
				t.Fatal(err)
			}
			r, err := c.AddTemplate(context.Background(), tt.Template)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %v; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
			expectedCount := 0
			expectedHandled := make(map[string]bool)
			if !tt.ErrorExpected {
				expectedCount = 1
				expectedHandled = map[string]bool{"h1": true}
			}
			if r.HandledCount() != expectedCount {
				t.Errorf("HandledCount() = %v; want %v", r.HandledCount(), expectedCount)
			}
			if !reflect.DeepEqual(r.Handled, expectedHandled) {
				t.Errorf("r.Handled = %v; want %v", r.Handled, expectedHandled)
			}
		})
	}
}

func TestRemoveTemplate(t *testing.T) {
	badRegoTempl := createTemplate(name("fake"), crdNames("Fake", "fakes"), targets("h1"))
	badRegoTempl.Spec.Targets[0].Rego = "asd{"
	tc := []struct {
		Name          string
		Handler       TargetHandler
		Template      *v1alpha1.ConstraintTemplate
		ErrorExpected bool
	}{
		{
			Name:          "Good Template",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      createTemplate(name("fake"), crdNames("Fake", "fakes"), targets("h1")),
			ErrorExpected: false,
		},
		{
			Name:          "Unknown Target",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      createTemplate(name("fake"), crdNames("Fake", "fakes"), targets("h2")),
			ErrorExpected: true,
		},
		{
			Name:          "Bad CRD",
			Handler:       &badHandler{Name: "h1", HasLib: true},
			Template:      createTemplate(name("fake"), targets("h1")),
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			d := local.New()
			b, err := NewBackend(Driver(d))
			if err != nil {
				t.Fatalf("Could not create backend: %s", err)
			}
			c, err := b.NewClient(Targets(tt.Handler))
			if err != nil {
				t.Fatal(err)
			}
			r, err := c.RemoveTemplate(context.Background(), tt.Template)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %v; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
			expectedCount := 0
			expectedHandled := make(map[string]bool)
			if !tt.ErrorExpected {
				expectedCount = 1
				expectedHandled = map[string]bool{"h1": true}
			}
			if r.HandledCount() != expectedCount {
				t.Errorf("HandledCount() = %v; want %v", r.HandledCount(), expectedCount)
			}
			if !reflect.DeepEqual(r.Handled, expectedHandled) {
				t.Errorf("r.Handled = %v; want %v", r.Handled, expectedHandled)
			}
		})
	}
}

func TestAddConstraint(t *testing.T) {
	tc := []struct {
		Name          string
		Constraint    *unstructured.Unstructured
		OmitTemplate  bool
		ErrorExpected bool
	}{
		{
			Name:       "Good Constraint",
			Constraint: newConstraint("Foo", "foo", nil),
		},
		{
			Name:          "No Name",
			Constraint:    newConstraint("Foo", "", nil),
			ErrorExpected: true,
		},
		{
			Name:          "No Kind",
			Constraint:    newConstraint("", "foo", nil),
			ErrorExpected: true,
		},
		{
			Name:          "No Template",
			Constraint:    newConstraint("Foo", "foo", nil),
			OmitTemplate:  true,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			d := local.New()
			b, err := NewBackend(Driver(d))
			if err != nil {
				t.Fatalf("Could not create backend: %s", err)
			}
			handler := &badHandler{Name: "h1", HasLib: true}
			c, err := b.NewClient(Targets(handler))
			if err != nil {
				t.Fatal(err)
			}
			if !tt.OmitTemplate {
				tmpl := createTemplate(name("foos"), crdNames("Foo", "foos"), targets("h1"))
				_, err := c.AddTemplate(context.Background(), tmpl)
				if err != nil {
					t.Fatal(err)
				}
			}
			r, err := c.AddConstraint(context.Background(), tt.Constraint)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %v; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
			expectedCount := 0
			expectedHandled := make(map[string]bool)
			if !tt.ErrorExpected {
				expectedCount = 1
				expectedHandled = map[string]bool{"h1": true}
			}
			if r.HandledCount() != expectedCount {
				t.Errorf("HandledCount() = %v; want %v", r.HandledCount(), expectedCount)
			}
			if !reflect.DeepEqual(r.Handled, expectedHandled) {
				t.Errorf("r.Handled = %v; want %v", r.Handled, expectedHandled)
			}
		})
	}
}

func TestRemoveConstraint(t *testing.T) {
	tc := []struct {
		Name          string
		Constraint    *unstructured.Unstructured
		OmitTemplate  bool
		ErrorExpected bool
	}{
		{
			Name:       "Good Constraint",
			Constraint: newConstraint("Foo", "foo", nil),
		},
		{
			Name:          "No Name",
			Constraint:    newConstraint("Foo", "", nil),
			ErrorExpected: true,
		},
		{
			Name:          "No Kind",
			Constraint:    newConstraint("", "foo", nil),
			ErrorExpected: true,
		},
		{
			Name:          "No Template",
			Constraint:    newConstraint("Foo", "foo", nil),
			OmitTemplate:  true,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			d := local.New()
			b, err := NewBackend(Driver(d))
			if err != nil {
				t.Fatalf("Could not create backend: %s", err)
			}
			handler := &badHandler{Name: "h1", HasLib: true}
			c, err := b.NewClient(Targets(handler))
			if err != nil {
				t.Fatal(err)
			}
			if !tt.OmitTemplate {
				tmpl := createTemplate(name("foos"), crdNames("Foo", "foos"), targets("h1"))
				_, err := c.AddTemplate(context.Background(), tmpl)
				if err != nil {
					t.Fatal(err)
				}
			}
			r, err := c.RemoveConstraint(context.Background(), tt.Constraint)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %v; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
			expectedCount := 0
			expectedHandled := make(map[string]bool)
			if !tt.ErrorExpected {
				expectedCount = 1
				expectedHandled = map[string]bool{"h1": true}
			}
			if r.HandledCount() != expectedCount {
				t.Errorf("HandledCount() = %v; want %v", r.HandledCount(), expectedCount)
			}
			if !reflect.DeepEqual(r.Handled, expectedHandled) {
				t.Errorf("r.Handled = %v; want %v", r.Handled, expectedHandled)
			}
		})
	}
}
