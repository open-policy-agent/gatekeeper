package transform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	rSchema "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
	"k8s.io/apiserver/pkg/cel/environment"
	"k8s.io/utils/ptr"
)

func shouldMatch(shouldMatch, shouldErr bool, matchResult matchconditions.MatchResult) error {
	hadErr := false
	if matchResult.Error != nil {
		hadErr = true
		if !shouldErr {
			return fmt.Errorf("failed to evaluate matcher: %w", matchResult.Error)
		}
	}
	if matchResult.Matches != shouldMatch {
		return fmt.Errorf("got matches = %v; wanted %v", matchResult.Matches, shouldMatch)
	}
	if !hadErr && shouldErr {
		return errors.New("expected error, not none")
	}
	return nil
}

type kindMatchEntry struct {
	Kinds  []string `json:"kinds,omitempty"`
	Groups []string `json:"apiGroups,omitempty"`
}

type kindMatcher []kindMatchEntry

func (km *kindMatcher) ToUnstructured() (interface{}, error) {
	bytes, err := json.Marshal(km)
	if err != nil {
		return nil, err
	}
	var ret interface{}
	if err := json.Unmarshal(bytes, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

type nsMatcher []string

func (nm *nsMatcher) ToUnstructured() (interface{}, error) {
	bytes, err := json.Marshal(nm)
	if err != nil {
		return nil, err
	}
	var ret interface{}
	if err := json.Unmarshal(bytes, &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

func TestMatchKinds(t *testing.T) {
	tests := []struct {
		name        string
		matcher     kindMatcher
		group       string
		kind        string
		shouldMatch bool
		shouldErr   bool
	}{
		{
			name:        "No match criteria",
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
		{
			name: "Kind glob",
			matcher: kindMatcher{
				{
					Kinds: []string{"*"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
		{
			name: "Group glob",
			matcher: kindMatcher{
				{
					Groups: []string{"*"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
		{
			name: "Both glob",
			matcher: kindMatcher{
				{
					Groups: []string{"*"},
					Kinds:  []string{"*"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
		{
			name: "Kind match",
			matcher: kindMatcher{
				{
					Kinds: []string{"BarKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
		{
			name: "Kind mismatch",
			matcher: kindMatcher{
				{
					Kinds: []string{"MissingKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: false,
		},
		{
			name: "Group match",
			matcher: kindMatcher{
				{
					Groups: []string{"FooGroup"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
		{
			name: "Group mismatch",
			matcher: kindMatcher{
				{
					Groups: []string{"MissingGroup"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: false,
		},
		{
			name: "Both match",
			matcher: kindMatcher{
				{
					Groups: []string{"FooGroup"},
					Kinds:  []string{"BarKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
		{
			name: "Both asserted, kind mismatch",
			matcher: kindMatcher{
				{
					Groups: []string{"FooGroup"},
					Kinds:  []string{"MissingKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: false,
		},
		{
			name: "Both asserted, group mismatch",
			matcher: kindMatcher{
				{
					Groups: []string{"MissingGroup"},
					Kinds:  []string{"BarKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: false,
		},
		{
			name: "Both asserted, both mismatch",
			matcher: kindMatcher{
				{
					Groups: []string{"MissingGroup"},
					Kinds:  []string{"MissingKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: false,
		},
		{
			name: "Group glob, kind mismatch",
			matcher: kindMatcher{
				{
					Groups: []string{"*"},
					Kinds:  []string{"MissingKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: false,
		},
		{
			name: "Kind glob, group mismatch",
			matcher: kindMatcher{
				{
					Groups: []string{"MissingGroup"},
					Kinds:  []string{"*"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: false,
		},
		{
			name: "Match in second criteria",
			matcher: kindMatcher{
				{
					Groups: []string{"MissingGroup"},
					Kinds:  []string{"*"},
				},
				{
					Groups: []string{"FooGroup"},
					Kinds:  []string{"BarKind"},
				},
			},
			group:       "FooGroup",
			kind:        "BarKind",
			shouldMatch: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filterCompiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
			if err != nil {
				t.Fatal(err)
			}
			celOpts := cel.OptionalVariableDeclarations{HasParams: true}
			filterCompiler.CompileAndStoreVariables(AllVariablesCEL(), celOpts, environment.StoredExpressions)
			matcher := matchconditions.NewMatcher(filterCompiler.CompileCondition(MatchKindsCEL(), celOpts, environment.StoredExpressions), ptr.To[v1.FailurePolicyType](v1.Fail), "matchTest", "kind", test.name)

			obj := &unstructured.Unstructured{}
			obj.SetName("RandomName")
			obj.SetGroupVersionKind(rSchema.GroupVersionKind{Group: test.group, Kind: test.kind})
			objBytes, err := json.Marshal(obj.Object)
			if err != nil {
				t.Fatal(err)
			}
			request := &admissionv1.AdmissionRequest{
				Kind:   metav1.GroupVersionKind{Group: test.group, Kind: test.kind},
				Object: runtime.RawExtension{Raw: objBytes},
			}
			versionedAttributes, err := RequestToVersionedAttributes(request)
			if err != nil {
				t.Fatal(err)
			}

			constraint := &unstructured.Unstructured{Object: map[string]interface{}{}}
			if test.matcher != nil {
				val, err := test.matcher.ToUnstructured()
				if err != nil {
					t.Fatal(err)
				}
				if err := unstructured.SetNestedField(constraint.Object, val, "spec", "match", "kinds"); err != nil {
					t.Fatal(err)
				}
			}

			if err := shouldMatch(test.shouldMatch, test.shouldErr, matcher.Match(context.Background(), versionedAttributes, constraint, nil)); err != nil {
				t.Error(err)
			}
			checkSpecializedMatch(t, MatchKindsV1Beta1(), constraint, request, test.shouldMatch, test.shouldErr)
		})
	}
}

func TestMatchNameGlob(t *testing.T) {
	tests := []struct {
		name         string
		matcher      *string
		objName      *string
		generateName *string
		shouldMatch  bool
		shouldErr    bool
	}{
		{
			name:        "No match criteria",
			shouldMatch: true,
		},
		{
			name:      "Match against missing metadata",
			matcher:   ptr.To[string]("somename"),
			shouldErr: true,
		},
		{
			name:        "Simple match",
			matcher:     ptr.To[string]("somename"),
			objName:     ptr.To[string]("somename"),
			shouldMatch: true,
		},
		{
			name:        "Literal dot matches",
			matcher:     ptr.To("foo.bar"),
			objName:     ptr.To("foo.bar"),
			shouldMatch: true,
		},
		{
			name:    "Literal dot is not a wildcard",
			matcher: ptr.To("foo.bar"),
			objName: ptr.To("foo-bar"),
		},
		{
			name:        "Dotted prefix matches",
			matcher:     ptr.To("foo.*"),
			objName:     ptr.To("foo.bar"),
			shouldMatch: true,
		},
		{
			name:    "Dotted prefix keeps literal dot",
			matcher: ptr.To("foo.*"),
			objName: ptr.To("foo-bar"),
		},
		{
			name:        "Dotted suffix matches",
			matcher:     ptr.To("*.bar"),
			objName:     ptr.To("foo.bar"),
			shouldMatch: true,
		},
		{
			name:    "Dotted suffix keeps literal dot",
			matcher: ptr.To("*.bar"),
			objName: ptr.To("foo-bar"),
		},
		{
			name:        "Dotted midfix matches",
			matcher:     ptr.To("*foo.bar*"),
			objName:     ptr.To("prefix-foo.bar-suffix"),
			shouldMatch: true,
		},
		{
			name:    "Dotted midfix keeps literal dot",
			matcher: ptr.To("*foo.bar*"),
			objName: ptr.To("prefix-foo-bar-suffix"),
		},
		{
			name:         "Dotted generateName matches",
			matcher:      ptr.To("foo.*"),
			generateName: ptr.To("foo.bar-"),
			shouldMatch:  true,
		},
		{
			name:         "Dotted generateName keeps literal dot",
			matcher:      ptr.To("foo.*"),
			generateName: ptr.To("foo-bar-"),
		},
		{
			name:        "No midfix without glob",
			matcher:     ptr.To[string]("middle"),
			objName:     ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "No prefix without glob",
			matcher:     ptr.To[string]("some"),
			objName:     ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "No postfix without glob",
			matcher:     ptr.To[string]("some"),
			objName:     ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "Midfix",
			matcher:     ptr.To[string]("*middle*"),
			objName:     ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "Prefix",
			matcher:     ptr.To[string]("some*"),
			objName:     ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "Postfix",
			matcher:     ptr.To[string]("*name"),
			objName:     ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:         "No explicit match with generateName",
			matcher:      ptr.To[string]("somemiddlename"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  false,
		},
		{
			name:         "No midfix match with generateName without glob",
			matcher:      ptr.To[string]("middle"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  false,
		},
		{
			name:         "No prefix match with generateName without glob",
			matcher:      ptr.To[string]("some"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  false,
		},
		{
			name:         "No suffix match with generateName without glob",
			matcher:      ptr.To[string]("name"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  false,
		},
		{
			name:         "Explicit match with generate name with glob",
			matcher:      ptr.To[string]("somemiddlename*"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  true,
		},
		{
			name:         "Midfix match with generateName with glob",
			matcher:      ptr.To[string]("*middle*"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  true,
		},
		{
			name:         "Prefix match with generateName with glob",
			matcher:      ptr.To[string]("some*"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  true,
		},
		{
			name:         "No suffix match with generateName with glob",
			matcher:      ptr.To[string]("*name"),
			generateName: ptr.To[string]("somemiddlename"),
			shouldMatch:  false,
		},
	}
	for _, test := range tests {
		obj := &unstructured.Unstructured{}
		if test.objName != nil {
			obj.SetName(*test.objName)
		}
		if test.generateName != nil {
			obj.SetGenerateName(*test.generateName)
		}
		obj.SetGroupVersionKind(rSchema.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"})

		objBytes, err := json.Marshal(obj.Object)
		if err != nil {
			t.Fatal(err)
		}
		raw := &runtime.RawExtension{Raw: objBytes}

		makeRequest := func(obj, oldObject *runtime.RawExtension) *admissionv1.AdmissionRequest {
			req := &admissionv1.AdmissionRequest{}

			if test.objName != nil {
				req.Name = *test.objName
			}

			if obj != nil {
				req.Object = *obj
			}

			if oldObject != nil {
				req.OldObject = *oldObject
			}

			return req
		}

		expandedTests := []struct {
			name    string
			request *admissionv1.AdmissionRequest
		}{
			{
				name:    fmt.Sprintf("%s_obj", test.name),
				request: makeRequest(raw.DeepCopy(), nil),
			},
			{
				name:    fmt.Sprintf("%s_oldObj", test.name),
				request: makeRequest(nil, raw.DeepCopy()),
			},
			{
				name:    fmt.Sprintf("%s_obj_oldObj", test.name),
				request: makeRequest(raw.DeepCopy(), raw.DeepCopy()),
			},
		}

		for _, subTest := range expandedTests {
			t.Run(subTest.name, func(t *testing.T) {
				filterCompiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
				if err != nil {
					t.Fatal(err)
				}
				celOpts := cel.OptionalVariableDeclarations{HasParams: true}
				filterCompiler.CompileAndStoreVariables(AllVariablesCEL(), celOpts, environment.StoredExpressions)
				matcher := matchconditions.NewMatcher(filterCompiler.CompileCondition(MatchNameGlobCEL(), celOpts, environment.StoredExpressions), ptr.To[v1.FailurePolicyType](v1.Fail), "matchTest", "name", test.name)

				constraint := &unstructured.Unstructured{Object: map[string]interface{}{}}
				if test.matcher != nil {
					if err := unstructured.SetNestedField(constraint.Object, *test.matcher, "spec", "match", "name"); err != nil {
						t.Fatal(err)
					}
				}

				versionedAttributes, err := RequestToVersionedAttributes(subTest.request)
				if err != nil {
					t.Fatal(err)
				}

				if err := shouldMatch(test.shouldMatch, test.shouldErr, matcher.Match(context.Background(), versionedAttributes, constraint, nil)); err != nil {
					t.Error(err)
				}
				checkSpecializedMatch(t, MatchNameGlobV1Beta1(), constraint, subTest.request, test.shouldMatch, test.shouldErr)
			})
		}
	}
}

func TestMatchNamespacesGlob(t *testing.T) {
	tests := []struct {
		name        string
		matcher     nsMatcher
		namespace   *string
		shouldMatch bool
		shouldErr   bool
		noMetadata  bool
	}{
		{
			name:        "No match criteria",
			shouldMatch: true,
		},
		{
			name:       "No metadata errors",
			matcher:    nsMatcher{"somename"},
			shouldErr:  true,
			noMetadata: true,
		},
		{
			name:        "Match against cluster scoped",
			matcher:     nsMatcher{"somename"},
			namespace:   ptr.To[string](""),
			shouldMatch: true,
		},
		{
			name:        "Match against missing metadata",
			matcher:     nsMatcher{"somename"},
			shouldMatch: true,
		},
		{
			name:        "Simple match",
			matcher:     nsMatcher{"somename"},
			namespace:   ptr.To[string]("somename"),
			shouldMatch: true,
		},
		{
			name:        "No midfix without glob",
			matcher:     nsMatcher{"middle"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "No prefix without glob",
			matcher:     nsMatcher{"some"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "No postfix without glob",
			matcher:     nsMatcher{"some"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "Midfix",
			matcher:     nsMatcher{"*middle*"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "Prefix",
			matcher:     nsMatcher{"some*"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "Postfix",
			matcher:     nsMatcher{"*name"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "No match multiple",
			matcher:     nsMatcher{"name", "othername"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "Match in second entry",
			matcher:     nsMatcher{"name", "somemiddlename"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
	}
	for _, test := range tests {
		obj := &unstructured.Unstructured{}
		if !test.noMetadata {
			obj.SetName("RandomName")
		}

		if test.namespace != nil {
			obj.SetNamespace(*test.namespace)
		}

		obj.SetGroupVersionKind(rSchema.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"})
		objBytes, err := json.Marshal(obj.Object)
		if err != nil {
			t.Fatal(err)
		}
		raw := &runtime.RawExtension{Raw: objBytes}

		makeRequest := func(obj, oldObject *runtime.RawExtension) *admissionv1.AdmissionRequest {
			req := &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"},
				Name: "RandomName",
			}
			if test.namespace != nil {
				req.Namespace = *test.namespace
			}

			if obj != nil {
				req.Object = *obj
			}

			if oldObject != nil {
				req.OldObject = *oldObject
			}

			return req
		}

		expandedTests := []struct {
			name    string
			request *admissionv1.AdmissionRequest
		}{
			{
				name:    fmt.Sprintf("%s_obj", test.name),
				request: makeRequest(raw.DeepCopy(), nil),
			},
			{
				name:    fmt.Sprintf("%s_oldObj", test.name),
				request: makeRequest(nil, raw.DeepCopy()),
			},
			{
				name:    fmt.Sprintf("%s_obj_oldObj", test.name),
				request: makeRequest(raw.DeepCopy(), raw.DeepCopy()),
			},
		}

		for _, subTest := range expandedTests {
			t.Run(subTest.name, func(t *testing.T) {
				filterCompiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
				if err != nil {
					t.Fatal(err)
				}
				celOpts := cel.OptionalVariableDeclarations{HasParams: true}
				filterCompiler.CompileAndStoreVariables(AllVariablesCEL(), celOpts, environment.StoredExpressions)
				matcher := matchconditions.NewMatcher(filterCompiler.CompileCondition(MatchNamespacesGlobCEL(), celOpts, environment.StoredExpressions), ptr.To[v1.FailurePolicyType](v1.Fail), "matchTest", "name", test.name)

				versionedAttributes, err := RequestToVersionedAttributes(subTest.request)
				if err != nil {
					t.Fatal(err)
				}

				constraint := &unstructured.Unstructured{Object: map[string]interface{}{}}
				if test.matcher != nil {
					val, err := test.matcher.ToUnstructured()
					if err != nil {
						t.Fatal(err)
					}
					if err := unstructured.SetNestedField(constraint.Object, val, "spec", "match", "namespaces"); err != nil {
						t.Fatal(err)
					}
				}

				if err := shouldMatch(test.shouldMatch, test.shouldErr, matcher.Match(context.Background(), versionedAttributes, constraint, nil)); err != nil {
					t.Error(err)
				}
				checkSpecializedMatch(t, MatchNamespacesGlobV1Beta1(), constraint, subTest.request, test.shouldMatch, test.shouldErr)
			})
		}
	}
}

func TestMatchExcludedNamespacesGlob(t *testing.T) {
	tests := []struct {
		name        string
		matcher     nsMatcher
		namespace   *string
		shouldMatch bool
		shouldErr   bool
		noMetadata  bool
	}{
		{
			name:        "No match criteria",
			shouldMatch: true,
		},
		{
			name:       "No metadata errors",
			matcher:    nsMatcher{"somename"},
			shouldErr:  true,
			noMetadata: true,
		},
		{
			name:        "Match against cluster scoped",
			matcher:     nsMatcher{"somename"},
			namespace:   ptr.To[string](""),
			shouldMatch: true,
		},
		{
			name:        "Match against missing namespace",
			matcher:     nsMatcher{"somename"},
			shouldMatch: true,
		},
		{
			name:        "Simple match",
			matcher:     nsMatcher{"somename"},
			namespace:   ptr.To[string]("somename"),
			shouldMatch: false,
		},
		{
			name:        "No midfix without glob",
			matcher:     nsMatcher{"middle"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "No prefix without glob",
			matcher:     nsMatcher{"some"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "No postfix without glob",
			matcher:     nsMatcher{"some"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "Midfix",
			matcher:     nsMatcher{"*middle*"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "Prefix",
			matcher:     nsMatcher{"some*"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "Postfix",
			matcher:     nsMatcher{"*name"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
		{
			name:        "No match multiple",
			matcher:     nsMatcher{"name", "othername"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: true,
		},
		{
			name:        "Match in second entry",
			matcher:     nsMatcher{"name", "somemiddlename"},
			namespace:   ptr.To[string]("somemiddlename"),
			shouldMatch: false,
		},
	}
	for _, test := range tests {
		obj := &unstructured.Unstructured{}
		if !test.noMetadata {
			obj.SetName("RandomName")
		}

		if test.namespace != nil {
			obj.SetNamespace(*test.namespace)
		}

		obj.SetGroupVersionKind(rSchema.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"})
		objBytes, err := json.Marshal(obj.Object)
		if err != nil {
			t.Fatal(err)
		}
		raw := &runtime.RawExtension{Raw: objBytes}

		makeRequest := func(obj, oldObject *runtime.RawExtension) *admissionv1.AdmissionRequest {
			req := &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"},
				Name: "RandomName",
			}
			if test.namespace != nil {
				req.Namespace = *test.namespace
			}

			if obj != nil {
				req.Object = *obj
			}

			if oldObject != nil {
				req.OldObject = *oldObject
			}

			return req
		}

		expandedTests := []struct {
			name    string
			request *admissionv1.AdmissionRequest
		}{
			{
				name:    fmt.Sprintf("%s_obj", test.name),
				request: makeRequest(raw.DeepCopy(), nil),
			},
			{
				name:    fmt.Sprintf("%s_oldObj", test.name),
				request: makeRequest(nil, raw.DeepCopy()),
			},
			{
				name:    fmt.Sprintf("%s_obj_oldObj", test.name),
				request: makeRequest(raw.DeepCopy(), raw.DeepCopy()),
			},
		}

		for _, subTest := range expandedTests {
			t.Run(subTest.name, func(t *testing.T) {
				filterCompiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
				if err != nil {
					t.Fatal(err)
				}
				celOpts := cel.OptionalVariableDeclarations{HasParams: true}
				filterCompiler.CompileAndStoreVariables(AllVariablesCEL(), celOpts, environment.StoredExpressions)
				matcher := matchconditions.NewMatcher(filterCompiler.CompileCondition(MatchExcludedNamespacesGlobCEL(), celOpts, environment.StoredExpressions), ptr.To[v1.FailurePolicyType](v1.Fail), "matchTest", "name", test.name)

				versionedAttributes, err := RequestToVersionedAttributes(subTest.request)
				if err != nil {
					t.Fatal(err)
				}

				constraint := &unstructured.Unstructured{Object: map[string]interface{}{}}
				if test.matcher != nil {
					val, err := test.matcher.ToUnstructured()
					if err != nil {
						t.Fatal(err)
					}
					if err := unstructured.SetNestedField(constraint.Object, val, "spec", "match", "excludedNamespaces"); err != nil {
						t.Fatal(err)
					}
				}

				if err := shouldMatch(test.shouldMatch, test.shouldErr, matcher.Match(context.Background(), versionedAttributes, constraint, nil)); err != nil {
					t.Error(err)
				}
				checkSpecializedMatch(t, MatchExcludedNamespacesGlobV1Beta1(), constraint, subTest.request, test.shouldMatch, test.shouldErr)
			})
		}
	}
}

func TestGlobRegexLiterals(t *testing.T) {
	const (
		nameField       = "name"
		namespacesField = "namespaces"
	)
	matchers := []struct {
		field     string
		condition func(string) admissionregistrationv1beta1.MatchCondition
		excluded  bool
	}{
		{field: nameField, condition: func(string) admissionregistrationv1beta1.MatchCondition { return MatchNameGlobV1Beta1() }},
		{field: namespacesField, condition: func(string) admissionregistrationv1beta1.MatchCondition { return MatchNamespacesGlobV1Beta1() }},
		{field: matchExcludedNamespacesField, condition: func(string) admissionregistrationv1beta1.MatchCondition { return MatchExcludedNamespacesGlobV1Beta1() }, excluded: true},
		{field: "global exclusions", condition: MatchGlobalExcludedNamespacesGlobV1Beta1, excluded: true},
		{field: "global exemptions", condition: MatchGlobalExemptedNamespacesGlobV1Beta1, excluded: true},
	}
	for _, match := range matchers {
		for _, literal := range []string{"foo.bar", `foo.+?()|[]{}^$bar`, `foo\bar`, `foo\Ebar`, `foo\\Ebar`, `foo\Qbar`} {
			for _, glob := range []string{literal, literal + "*", "*" + literal, "*" + literal + "*"} {
				t.Run(fmt.Sprintf("%s/%q", match.field, glob), func(t *testing.T) {
					condition := match.condition(strconv.Quote(glob))
					compiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
					if err != nil {
						t.Fatal(err)
					}
					evaluator := compiler.CompileCondition([]cel.ExpressionAccessor{&matchconditions.MatchCondition{
						Name: condition.Name, Expression: condition.Expression,
					}}, cel.OptionalVariableDeclarations{HasParams: true}, environment.StoredExpressions)
					if errs := evaluator.CompilationErrors(); len(errs) != 0 {
						t.Fatalf("shared matcher does not compile: %v", errs)
					}
					matcher := matchconditions.NewMatcher(evaluator, ptr.To(v1.Fail), "matchTest", "literal", condition.Name)
					constraint := &unstructured.Unstructured{Object: map[string]interface{}{}}
					switch match.field {
					case nameField:
						if err := unstructured.SetNestedField(constraint.Object, glob, "spec", "match", match.field); err != nil {
							t.Fatal(err)
						}
					case namespacesField, matchExcludedNamespacesField:
						if err := unstructured.SetNestedStringSlice(constraint.Object, []string{glob}, "spec", "match", match.field); err != nil {
							t.Fatal(err)
						}
					}
					for _, kind := range []string{"ConfigMap", "Namespace"} {
						for _, candidate := range []string{literal, literal + "-suffix", "prefix-" + literal, "prefix-" + literal + "-suffix", "foo-bar", "unrelated"} {
							t.Run(fmt.Sprintf("%s/%q", kind, candidate), func(t *testing.T) {
								object := &unstructured.Unstructured{}
								object.SetGroupVersionKind(rSchema.GroupVersionKind{Version: "v1", Kind: kind})
								object.SetName(candidate)
								object.SetLabels(map[string]string{"admission.gatekeeper.sh/ignore": ""})
								if kind != "Namespace" {
									object.SetNamespace(candidate)
								}
								encoded, err := json.Marshal(object.Object)
								if err != nil {
									t.Fatal(err)
								}
								request := &admissionv1.AdmissionRequest{
									Kind: metav1.GroupVersionKind{Version: "v1", Kind: kind}, Object: runtime.RawExtension{Raw: encoded},
								}
								attributes, err := RequestToVersionedAttributes(request)
								if err != nil {
									t.Fatal(err)
								}
								want := wildcard.Wildcard(glob).Matches(candidate)
								if match.excluded {
									want = !want
								}
								if kind == "Namespace" && (match.field == namespacesField || match.field == matchExcludedNamespacesField) {
									want = true
								}
								if err := shouldMatch(want, false, matcher.Match(context.Background(), attributes, constraint, nil)); err != nil {
									t.Error(err)
								}
								checkSpecializedMatch(t, condition, constraint, request, want, false)
							})
						}
					}
				})
			}
		}
	}
}

func checkSpecializedMatch(t *testing.T, condition admissionregistrationv1beta1.MatchCondition, constraint *unstructured.Unstructured, request *admissionv1.AdmissionRequest, wantMatch, wantError bool) {
	t.Helper()
	conditions, err := specializeMatchConditions([]admissionregistrationv1beta1.MatchCondition{condition}, constraint)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
	if err != nil {
		t.Fatal(err)
	}
	expressions := make([]cel.ExpressionAccessor, 0, len(conditions))
	for _, specialized := range conditions {
		expressions = append(expressions, &matchconditions.MatchCondition{Name: specialized.Name, Expression: specialized.Expression})
	}
	evaluator := compiler.CompileCondition(expressions, cel.OptionalVariableDeclarations{HasParams: false}, environment.StoredExpressions)
	if errs := evaluator.CompilationErrors(); len(errs) != 0 {
		t.Fatalf("specialized matcher does not compile: %v", errs)
	}
	matcher := matchconditions.NewMatcher(evaluator, ptr.To(v1.Fail), "matchTest", "specialized", condition.Name)
	attributes, err := RequestToVersionedAttributes(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := shouldMatch(wantMatch, wantError, matcher.Match(context.Background(), attributes, nil, nil)); err != nil {
		t.Errorf("specialized: %v", err)
	}
}

func UnstructuredWithValue(val interface{}, fields ...string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if err := unstructured.SetNestedField(obj.Object, val, fields...); err != nil {
		panic(fmt.Errorf("%w: while setting unstructured value", err))
	}
	return obj
}

func TestParamsBinding(t *testing.T) {
	tests := []struct {
		name         string
		constraint   *unstructured.Unstructured
		assertionCEL string
	}{
		{
			name:         "Params are defined",
			constraint:   UnstructuredWithValue(true, "spec", "parameters", "paramExists"),
			assertionCEL: "variables.params.paramExists == true",
		},
		{
			name:         "Params not defined, spec is defined",
			constraint:   UnstructuredWithValue(true, "spec"),
			assertionCEL: "variables.params == null",
		},
		{
			name:         "No spec",
			constraint:   UnstructuredWithValue(map[string]interface{}{}, "status"),
			assertionCEL: "variables.params == null",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filterCompiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
			if err != nil {
				t.Fatal(err)
			}
			celOpts := cel.OptionalVariableDeclarations{HasParams: true}
			filterCompiler.CompileAndStoreVariables(AllVariablesCEL(), celOpts, environment.StoredExpressions)
			matcher := matchconditions.NewMatcher(
				filterCompiler.CompileCondition(
					[]cel.ExpressionAccessor{
						&matchconditions.MatchCondition{
							Name:       "TestParams",
							Expression: test.assertionCEL,
						},
					},
					celOpts,
					environment.StoredExpressions,
				),
				ptr.To[v1.FailurePolicyType](v1.Fail),
				"matchTest",
				"name",
				test.name,
			)

			obj := &unstructured.Unstructured{}
			objName := "test-obj"
			obj.SetName(objName)
			obj.SetGroupVersionKind(rSchema.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"})
			objBytes, err := json.Marshal(obj.Object)
			if err != nil {
				t.Fatal(err)
			}
			request := &admissionv1.AdmissionRequest{
				Kind:   metav1.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"},
				Object: runtime.RawExtension{Raw: objBytes},
			}
			request.Name = objName

			versionedAttributes, err := RequestToVersionedAttributes(request)
			if err != nil {
				t.Fatal(err)
			}

			if err := shouldMatch(true, false, matcher.Match(context.Background(), versionedAttributes, test.constraint, nil)); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestObjectBinding(t *testing.T) {
	tests := []struct {
		name         string
		operation    *admissionv1.Operation
		objPopulated bool
		assertionCEL string
	}{
		{
			name:         "No operation, obj populated",
			assertionCEL: "variables.anyObject != null",
			objPopulated: true,
		},
		{
			name:         "No operation, obj not populated",
			assertionCEL: "variables.anyObject == null",
			objPopulated: false,
		},
		{
			name:         "DELETE operation, obj populated",
			assertionCEL: "variables.anyObject != null",
			operation:    ptr.To[admissionv1.Operation](admissionv1.Delete),
			objPopulated: false,
		},
		{
			name:         "Non-DELETE operation, obj not populated",
			assertionCEL: "variables.anyObject == null",
			operation:    ptr.To[admissionv1.Operation](admissionv1.Create),
			objPopulated: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filterCompiler, err := cel.NewCompositedCompiler(environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()))
			if err != nil {
				t.Fatal(err)
			}
			celOpts := cel.OptionalVariableDeclarations{HasParams: true}
			filterCompiler.CompileAndStoreVariables(AllVariablesCEL(), celOpts, environment.StoredExpressions)
			matcher := matchconditions.NewMatcher(
				filterCompiler.CompileCondition(
					[]cel.ExpressionAccessor{
						&matchconditions.MatchCondition{
							Name:       "TestObject",
							Expression: test.assertionCEL,
						},
					},
					celOpts,
					environment.StoredExpressions,
				),
				ptr.To[v1.FailurePolicyType](v1.Fail),
				"matchTest",
				"name",
				test.name,
			)

			obj := &unstructured.Unstructured{}
			objName := "test-obj"
			obj.SetName(objName)
			obj.SetGroupVersionKind(rSchema.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"})
			objBytes, err := json.Marshal(obj.Object)
			if err != nil {
				t.Fatal(err)
			}
			request := &admissionv1.AdmissionRequest{
				Kind:      metav1.GroupVersionKind{Group: "FooGroup", Kind: "BarKind"},
				OldObject: runtime.RawExtension{Raw: objBytes},
			}
			if test.objPopulated {
				request.Object = *request.OldObject.DeepCopy()
			}
			if test.operation != nil {
				request.Operation = *test.operation
			}
			request.Name = objName

			versionedAttributes, err := RequestToVersionedAttributes(request)
			if err != nil {
				t.Fatal(err)
			}

			constraint := &unstructured.Unstructured{Object: map[string]interface{}{}}
			if err := shouldMatch(true, false, matcher.Match(context.Background(), versionedAttributes, constraint, nil)); err != nil {
				t.Error(err)
			}
		})
	}
}
