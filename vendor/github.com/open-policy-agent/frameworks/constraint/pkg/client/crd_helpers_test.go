package client

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8schema "k8s.io/apimachinery/pkg/runtime/schema"
)

// helpers for creating a ConstraintTemplate for test

type tmplArg func(*templates.ConstraintTemplate)

func name(name string) tmplArg {
	return func(tmpl *templates.ConstraintTemplate) {
		tmpl.ObjectMeta.Name = name
	}
}

func crdNames(kind string) tmplArg {
	return func(tmpl *templates.ConstraintTemplate) {
		tmpl.Spec.CRD.Spec.Names = templates.Names{
			Kind: kind,
		}
	}
}

func schema(pm propMap) tmplArg {
	p := prop(pm)
	return func(tmpl *templates.ConstraintTemplate) {
		tmpl.Spec.CRD.Spec.Validation = &templates.Validation{}
		tmpl.Spec.CRD.Spec.Validation.OpenAPIV3Schema = &p
	}
}

func targets(ts ...string) tmplArg {
	targets := make([]templates.Target, len(ts))
	for i, t := range ts {
		targets[i] = templates.Target{Target: t, Rego: `package hello violation[{"msg": msg}] {msg = "hello"}`}
	}

	return func(tmpl *templates.ConstraintTemplate) {
		tmpl.Spec.Targets = targets
	}
}

func createTemplate(args ...tmplArg) *templates.ConstraintTemplate {
	tmpl := &templates.ConstraintTemplate{}
	for _, arg := range args {
		arg(tmpl)
	}
	return tmpl
}

// Minimal implementation of a target handler needed for CRD helpers

type targetHandlerArg func(*testTargetHandler)

func matchSchema(pm propMap) targetHandlerArg {
	return func(h *testTargetHandler) {
		h.matchSchema = prop(pm)
	}
}

var _ MatchSchemaProvider = &testTargetHandler{}

type testTargetHandler struct {
	matchSchema apiextensions.JSONSchemaProps
}

func createTestTargetHandler(args ...targetHandlerArg) MatchSchemaProvider {
	h := &testTargetHandler{}
	for _, arg := range args {
		arg(h)
	}
	return h
}

func (h testTargetHandler) MatchSchema() apiextensions.JSONSchemaProps {
	return h.matchSchema
}

// schema Helpers

type propMap map[string]apiextensions.JSONSchemaProps

// prop currently expects 0 or 1 prop map. More is unsupported.
func prop(pm ...map[string]apiextensions.JSONSchemaProps) apiextensions.JSONSchemaProps {
	if len(pm) == 0 {
		return apiextensions.JSONSchemaProps{}
	}
	return apiextensions.JSONSchemaProps{Properties: pm[0]}
}

// tProp creates a typed property
func tProp(t string) apiextensions.JSONSchemaProps {
	return apiextensions.JSONSchemaProps{Type: t}
}

func expectedSchema(pm propMap) *apiextensions.JSONSchemaProps {
	p := prop(propMap{"spec": prop(pm)})
	return &p
}

// Custom Resource Helpers

type customResourceArg func(u *unstructured.Unstructured)

func gvk(group, version, kind string) customResourceArg {
	return func(u *unstructured.Unstructured) {
		u.SetGroupVersionKind(k8schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	}
}

func kind(kind string) customResourceArg {
	return gvk(constraintGroup, "v1beta1", kind)
}

func params(s string) customResourceArg {
	p := map[string]interface{}{}
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		panic(fmt.Sprintf("bad JSON in test: %s: %s", s, err))
	}
	return func(u *unstructured.Unstructured) {
		unstructured.SetNestedField(u.Object, p, "spec", "parameters")
	}
}

func match(s string) customResourceArg {
	m := map[string]interface{}{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(fmt.Sprintf("bad JSON in test: %s: %s", s, err))
	}
	return func(u *unstructured.Unstructured) {
		unstructured.SetNestedField(u.Object, m, "spec", "match")
	}
}

func crName(name string) customResourceArg {
	return func(u *unstructured.Unstructured) {
		u.SetName(name)
	}
}

func createCR(args ...customResourceArg) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	for _, arg := range args {
		arg(cr)
	}
	return cr
}

// Tests

type crdTestCase struct {
	Name           string
	Template       *templates.ConstraintTemplate
	Handler        MatchSchemaProvider
	CR             *unstructured.Unstructured
	ExpectedSchema *apiextensions.JSONSchemaProps
	ErrorExpected  bool
}

func TestValidateTemplate(t *testing.T) {
	tests := []crdTestCase{
		{
			Name:          "Valid Template",
			Template:      createTemplate(targets("fooTarget")),
			ErrorExpected: false,
		},
		{
			Name:          "No Targets Fails",
			Template:      createTemplate(),
			ErrorExpected: true,
		},
		{
			Name:          "Two Targets Fails",
			Template:      createTemplate(targets("fooTarget", "barTarget")),
			ErrorExpected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			err := validateTargets(tc.Template)
			if (err == nil) && tc.ErrorExpected {
				t.Errorf("err = nil; want non-nil")
			}
			if (err != nil) && !tc.ErrorExpected {
				t.Errorf("err = \"%s\"; want nil", err)
			}
		})
	}
}

func TestCreateSchema(t *testing.T) {
	tests := []crdTestCase{
		{
			Name:           "No Schema",
			Template:       createTemplate(),
			Handler:        createTestTargetHandler(),
			ExpectedSchema: expectedSchema(propMap{"match": prop()}),
		},
		{
			Name:     "Just Match",
			Template: createTemplate(),
			Handler:  createTestTargetHandler(matchSchema(propMap{"labels": prop()})),
			ExpectedSchema: expectedSchema(propMap{
				"match": prop(propMap{
					"labels": prop()})}),
		},
		{
			Name:     "Just Parameters",
			Template: createTemplate(schema(propMap{"test": prop()})),
			Handler:  createTestTargetHandler(),
			ExpectedSchema: expectedSchema(propMap{
				"match": prop(),
				"parameters": prop(propMap{
					"test": prop(),
				}),
			}),
		},
		{
			Name:     "Match and Parameters",
			Template: createTemplate(schema(propMap{"dragon": prop()})),
			Handler:  createTestTargetHandler(matchSchema(propMap{"fire": prop()})),
			ExpectedSchema: expectedSchema(propMap{
				"match": prop(propMap{
					"fire": prop(),
				}),
				"parameters": prop(propMap{
					"dragon": prop(),
				}),
			}),
		},
	}
	for _, tc := range tests {
		h := newCRDHelper()
		t.Run(tc.Name, func(t *testing.T) {
			schema, err := h.createSchema(tc.Template, tc.Handler)
			if err != nil {
				t.Errorf("error = %v; want nil", err)
			}
			if !reflect.DeepEqual(schema, tc.ExpectedSchema) {
				t.Errorf("createSchema(%#v) = \n%#v; \nwant %#v", tc.Template, *schema, *tc.ExpectedSchema)
			}
		})
	}
}

func TestCRDCreationAndValidation(t *testing.T) {
	tests := []crdTestCase{
		{
			Name: "Most Basic Valid Template",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
			),
			Handler:       createTestTargetHandler(),
			ErrorExpected: false,
		},
		{
			Name: "Template With Parameter Schema",
			Template: createTemplate(
				name("morehorses"),
				crdNames("Horse"),
				schema(propMap{
					"coat":  prop(propMap{"color": prop(), "clean": prop()}),
					"speed": prop(),
				}),
			),
			Handler:       createTestTargetHandler(),
			ErrorExpected: false,
		},
		{
			Name: "Template With Parameter and Match Schema",
			Template: createTemplate(
				name("morehorses"),
				crdNames("Horse"),
				schema(propMap{
					"coat":  prop(propMap{"color": prop(), "clean": prop()}),
					"speed": prop(),
				}),
			),
			Handler: createTestTargetHandler(
				matchSchema(propMap{
					"namespace":     prop(),
					"labelSelector": prop(propMap{"matchLabels": prop()}),
				})),
			ErrorExpected: false,
		},
		{
			Name:          "No Kind Fails",
			Template:      createTemplate(),
			Handler:       createTestTargetHandler(),
			ErrorExpected: true,
		},
	}
	h := newCRDHelper()
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			schema, err := h.createSchema(tc.Template, tc.Handler)
			if err != nil {
				t.Errorf("err = %v; want nil", err)
			}
			crd, err := h.createCRD(tc.Template, schema)
			if err != nil {
				t.Errorf("err = %v; want nil", err)
			}
			err = h.validateCRD(crd)
			if (err == nil) && tc.ErrorExpected {
				t.Errorf("err = nil; want non-nil")
			}
			if (err != nil) && !tc.ErrorExpected {
				t.Errorf("err = \"%s\"; want nil", err)
			}
		})
	}
}

func TestCRValidation(t *testing.T) {
	tests := []crdTestCase{
		{
			Name: "Empty Schema and CR",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
			),
			Handler:       createTestTargetHandler(),
			CR:            createCR(crName("mycr"), kind("Horse")),
			ErrorExpected: false,
		},
		{
			Name: "Correct Prop Type",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
				schema(propMap{"fast": tProp("boolean")}),
			),
			Handler: createTestTargetHandler(),
			CR: createCR(
				crName("mycr"),
				kind("Horse"),
				params(`{"fast": true}`),
			),
			ErrorExpected: false,
		},
		{
			Name: "Correct Prop And Match Type",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
				schema(propMap{"fast": tProp("boolean")}),
			),
			Handler: createTestTargetHandler(
				matchSchema(propMap{"heavierThanLbs": tProp("number")}),
			),
			CR: createCR(
				crName("mycr"),
				kind("Horse"),
				params(`{"fast": true}`),
				match(`{"heavierThanLbs": 100}`),
			),
			ErrorExpected: false,
		},
		{
			Name: "No Name",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
			),
			Handler:       createTestTargetHandler(),
			CR:            createCR(kind("Horse")),
			ErrorExpected: true,
		},
		{
			Name: "Wrong Kind",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
			),
			Handler:       createTestTargetHandler(),
			CR:            createCR(crName("mycr"), kind("Cat")),
			ErrorExpected: true,
		},
		{
			Name: "Wrong Version",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
			),
			Handler:       createTestTargetHandler(),
			CR:            createCR(crName("mycr"), gvk(constraintGroup, "badversion", "Horse")),
			ErrorExpected: true,
		},
		{
			Name: "Wrong Group",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
			),
			Handler:       createTestTargetHandler(),
			CR:            createCR(crName("mycr"), gvk("badgroup", "v1alpha1", "Horse")),
			ErrorExpected: true,
		},
		{
			Name: "Wrong Prop Type",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
				schema(propMap{"fast": tProp("boolean")}),
			),
			Handler: createTestTargetHandler(),
			CR: createCR(
				crName("mycr"),
				kind("Horse"),
				params(`{"fast": "the fastest"}`),
			),
			ErrorExpected: true,
		},
		{
			Name: "Wrong Prop And Match Type",
			Template: createTemplate(
				name("SomeName"),
				crdNames("Horse"),
				schema(propMap{"fast": tProp("boolean")}),
			),
			Handler: createTestTargetHandler(
				matchSchema(propMap{"heavierThanLbs": tProp("number")}),
			),
			CR: createCR(
				crName("mycr"),
				kind("Horse"),
				params(`{"fast": true}`),
				match(`{"heavierThanLbs": "one hundred"}`),
			),
			ErrorExpected: true,
		},
	}
	h := newCRDHelper()
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			schema, err := h.createSchema(tc.Template, tc.Handler)
			if err != nil {
				t.Errorf("err = %v; want nil", err)
			}
			crd, err := h.createCRD(tc.Template, schema)
			if err != nil {
				t.Errorf("err = %v; want nil", err)
			}
			if err := h.validateCRD(crd); err != nil {
				t.Errorf("Bad test setup: Bad CRD: %s", err)
			}
			err = h.validateCR(tc.CR, crd)
			if (err == nil) && tc.ErrorExpected {
				t.Errorf("err = nil; want non-nil")
			}
			if (err != nil) && !tc.ErrorExpected {
				t.Errorf("err = \"%s\"; want nil", err)
			}
		})
	}
}
