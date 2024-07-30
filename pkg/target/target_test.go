package target

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestFrameworkInjection(t *testing.T) {
	target := &K8sValidationTarget{}
	driver, err := rego.New(rego.Tracing(true))
	if err != nil {
		t.Fatal(err)
	}

	_, err = constraintclient.NewClient(constraintclient.Targets(target), constraintclient.Driver(driver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}
}

func TestValidateConstraint(t *testing.T) {
	tc := []struct {
		Name          string
		Constraint    string
		ErrorExpected bool
	}{
		{
			Name: "No LabelSelector",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sRequiredLabel",
	"metadata": {
		"name": "ns-must-have-gk"
	},
	"spec": {
		"match": {
			"source": "All",
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Namespace"]
				}
			]
		},
		"parameters": {
			"labels": ["gatekeeper"]
		}
	}
}
`,
			ErrorExpected: false,
		},
		{
			Name: "Valid LabelSelector",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sRequiredLabel",
	"metadata": {
		"name": "ns-must-have-gk"
	},
	"spec": {
		"match": {
			"source": "Original",
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Namespace"]
				}
			],
			"labelSelector": {
				"matchExpressions": [{
					"key": "someKey",
					"operator": "NotIn",
					"values": ["some-value"]
				}]
			}
		},
		"parameters": {
			"labels": ["gatekeeper"]
		}
	}
}
`,
			ErrorExpected: false,
		},
		{
			Name: "Invalid LabelSelector type",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sRequiredLabel",
	"metadata": {
		"name": "ns-must-have-gk"
	},
	"spec": {
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Namespace"]
				}
			],
			"labelSelector": 3
		},
		"parameters": {
			"labels": ["gatekeeper"]
		}
	}
}
`,
			ErrorExpected: true,
		},
		{
			Name: "Invalid LabelSelector MatchLabels",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sRequiredLabel",
	"metadata": {
		"name": "ns-must-have-gk"
	},
	"spec": {
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Namespace"]
				}
			],
			"labelSelector": {
        "matchLabels": 3
      }
		},
		"parameters": {
			"labels": ["gatekeeper"]
		}
	}
}
`,
			ErrorExpected: true,
		},
		{
			Name: "Invalid LabelSelector",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sRequiredLabel",
	"metadata": {
		"name": "ns-must-have-gk"
	},
	"spec": {
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Namespace"]
				}
			],
			"labelSelector": {
				"matchExpressions": [{
					"key": "someKey",
					"operator": "Something Bad",
					"values": ["some value"]
				}]
			}
		},
		"parameters": {
			"labels": ["gatekeeper"]
		}
	}
}
`,
			ErrorExpected: true,
		},
		{
			Name: "No NamespaceSelector",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sAllowedRepos",
	"metadata": {
		"name": "prod-nslabels-is-openpolicyagent"
	},
	"spec": {
		"match": {
			"source": "Generated",
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Pod"]
				}
			],
			"labelSelector": {
				"matchExpressions": [{
					"key": "someKey",
					"operator": "In",
					"values": ["some-value"]
				}]
			}
		},
		"parameters": {
			"repos": ["openpolicyagent"]
		}
	}
}
`,
			ErrorExpected: false,
		},
		{
			Name: "Valid NamespaceSelector",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sAllowedRepos",
	"metadata": {
		"name": "prod-nslabels-is-openpolicyagent"
	},
	"spec": {
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Pod"]
				}
			],
			"namespaceSelector": {
				"matchExpressions": [{
					"key": "someKey",
					"operator": "In",
					"values": ["some-value"]
				}]
			}
		},
		"parameters": {
			"repos": ["openpolicyagent"]
		}
	}
}
`,
			ErrorExpected: false,
		},
		{
			Name: "Invalid NamespaceSelector type",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sAllowedRepos",
	"metadata": {
		"name": "prod-nslabels-is-openpolicyagent"
	},
	"spec": {
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Pod"]
				}
			],
			"namespaceSelector": 3
		},
		"parameters": {
			"repos": ["openpolicyagent"]
		}
	}
}
`,
			ErrorExpected: true,
		},
		{
			Name: "Invalid NamespaceSelector MatchLabel",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sAllowedRepos",
	"metadata": {
		"name": "prod-nslabels-is-openpolicyagent"
	},
	"spec": {
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Pod"]
				}
			],
			"namespaceSelector": {
        "matchLabels": 3
      }
		},
		"parameters": {
			"repos": ["openpolicyagent"]
		}
	}
}
`,
			ErrorExpected: true,
		},
		{
			Name: "Invalid NamespaceSelector",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sAllowedRepos",
	"metadata": {
		"name": "prod-nslabels-is-openpolicyagent"
	},
	"spec": {
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Pod"]
				}
			],
			"namespaceSelector": {
				"matchExpressions": [{
					"key": "someKey",
					"operator": "Blah",
					"values": ["some value"]
				}]
			}
		},
		"parameters": {
			"repos": ["openpolicyagent"]
		}
	}
}
`,
			ErrorExpected: true,
		},
		{
			Name: "Valid EnforcementAction",
			Constraint: `
{
	"apiVersion": "constraints.gatekeeper.sh/v1beta1",
	"kind": "K8sAllowedRepos",
	"metadata": {
		"name": "prod-nslabels-is-openpolicyagent"
	},
	"spec": {
		"enforcementAction": "dryrun",
		"match": {
			"kinds": [
				{
					"apiGroups": [""],
					"kinds": ["Pod"]
				}
			]
		},
		"parameters": {
			"repos": ["openpolicyagent"]
		}
	}
}
`,
			ErrorExpected: false,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			h := &K8sValidationTarget{}
			u := &unstructured.Unstructured{}
			err := json.Unmarshal([]byte(tt.Constraint), u)
			if err != nil {
				t.Fatalf("Unable to parse constraint JSON: %s", err)
			}
			err = h.ValidateConstraint(u)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %s; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
		})
	}
}

func TestProcessData(t *testing.T) {
	tc := []struct {
		name        string
		obj         interface{}
		wantHandled bool
		wantPath    []string
		wantErr     error
	}{
		{
			name:        "Cluster Object",
			obj:         makeResource(schema.GroupVersionKind{Version: "v1beta1", Kind: "Rock"}, "myrock"),
			wantHandled: true,
			wantPath:    []string{"cluster", "v1beta1", "Rock", "myrock"},
			wantErr:     nil,
		},
		{
			name:        "Namespaced Object",
			obj:         makeNamespacedResource(schema.GroupVersionKind{Version: "v1beta1", Kind: "Rock"}, "foo", "myrock"),
			wantHandled: true,
			wantPath:    []string{"namespace", "foo", "v1beta1", "Rock", "myrock"},
			wantErr:     nil,
		},
		{
			name:        "Grouped Object",
			obj:         makeResource(schema.GroupVersionKind{Group: "mygroup", Version: "v1beta1", Kind: "Rock"}, "myrock"),
			wantHandled: true,
			wantPath:    []string{"cluster", "mygroup/v1beta1", "Rock", "myrock"},
			wantErr:     nil,
		},
		{
			name:        "No Version",
			obj:         makeResource(schema.GroupVersionKind{Version: "", Kind: "Rock"}, "myrock"),
			wantHandled: true,
			wantPath:    nil,
			wantErr:     ErrRequestObject,
		},
		{
			name:        "No Kind",
			obj:         makeResource(schema.GroupVersionKind{Version: "v1beta1", Kind: ""}, "myrock"),
			wantHandled: true,
			wantPath:    nil,
			wantErr:     ErrRequestObject,
		},
		{
			name:        "Wipe Data",
			obj:         WipeData(),
			wantHandled: true,
			wantPath:    nil,
			wantErr:     nil,
		},
		{
			name:        "non-handled type",
			obj:         3,
			wantHandled: false,
			wantPath:    nil,
			wantErr:     nil,
		},
	}
	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			h := &K8sValidationTarget{}

			handled, path, data, err := h.ProcessData(tt.obj)
			if handled != tt.wantHandled {
				t.Errorf("got handled = %t, want %t", handled, tt.wantHandled)
			}

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got err %v, want %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(path, tt.wantPath); diff != "" {
				t.Error(diff)
			}

			if tt.wantErr == nil {
				switch obj := tt.obj.(type) {
				case *unstructured.Unstructured:
					if diff := cmp.Diff(obj.Object, data); diff != "" {
						t.Error(diff)
					}
				case wipeData:
					if data != nil {
						t.Errorf("data = %v; want nil", spew.Sdump(data))
					}
				}
				if err != nil {
					t.Errorf("err = %s; want nil", err)
				}
			} else if data != nil {
				t.Errorf("data = %v; want nil", spew.Sdump(data))
			}
		})
	}
}

func namespaceSelectorMatch() *match.Match {
	return &match.Match{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"ns": "label",
			},
		},
	}
}

func fooMatch() *match.Match {
	return &match.Match{
		Source: string(types.SourceTypeAll),
		Kinds: []match.Kinds{
			{
				Kinds:     []string{"Thing"},
				APIGroups: []string{"some"},
			},
		},
		Scope:              "Namespaced",
		Namespaces:         []wildcard.Wildcard{"my-ns"},
		ExcludedNamespaces: nil,
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"obj": "label",
			},
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"ns": "label",
			},
		},
		Name: "",
	}
}

func fooConstraint() *unstructured.Unstructured {
	return makeConstraint(
		setKinds([]string{"some"}, []string{"Thing"}),
		setScope("Namespaced"),
		setNamespaceName("my-ns"),
		setLabelSelector("obj", "label"),
		setNamespaceSelector("ns", "label"),
		setSource(string(types.SourceTypeDefault)),
	)
}

func invalidMatchConstraintType() *unstructured.Unstructured {
	cstr := makeConstraint()
	err := unstructured.SetNestedField(cstr.Object, 3.0, "spec", "match")
	if err != nil {
		panic(err)
	}
	return cstr
}

func invalidMatchConstraint() *unstructured.Unstructured {
	cstr := makeConstraint()
	err := unstructured.SetNestedField(cstr.Object, 3.0, "spec", "match", "kinds")
	if err != nil {
		panic(err)
	}
	return cstr
}

func TestToMatcher(t *testing.T) {
	unstructAssign, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(&unversioned.Assign{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{},
		Spec: unversioned.AssignSpec{
			Match: *fooMatch(),
		},
	})
	tests := []struct {
		name       string
		constraint *unstructured.Unstructured
		want       constraints.Matcher
		wantErr    error
	}{
		{
			name:       "constraint with no match fields",
			constraint: makeConstraint(),
			want:       &Matcher{},
			wantErr:    nil,
		},
		{
			name:       "constraint with match fields",
			constraint: fooConstraint(),
			want: &Matcher{
				match: fooMatch(),
				cache: newNsCache(),
			},
		},
		{
			name:       "constraint with invalid Match type",
			constraint: invalidMatchConstraintType(),
			want:       nil,
			wantErr:    ErrCreatingMatcher,
		},
		{
			name:       "constraint with invalid Match field type",
			constraint: invalidMatchConstraint(),
			want:       nil,
			wantErr:    ErrCreatingMatcher,
		},
		{
			name: "mutator with match fields",
			constraint: &unstructured.Unstructured{
				Object: unstructAssign,
			},
			want: &Matcher{
				match: fooMatch(),
				cache: newNsCache(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &K8sValidationTarget{}
			got, err := h.ToMatcher(tt.constraint)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("got ToMatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			opts := []cmp.Option{
				// Since nsCache is lazy-instantiated and this test isn't concerned
				// about caching functionality, we do not compare the cache
				cmpopts.IgnoreTypes(sync.RWMutex{}, nsCache{}),
				cmp.AllowUnexported(Matcher{}),
			}
			if diff := cmp.Diff(tt.want, got, opts...); diff != "" {
				t.Errorf("ToMatcher() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func matchedRawData() []byte {
	objData, _ := json.Marshal(makeNamespacedResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", "bar", map[string]string{"obj": "label"}).Object)
	return objData
}

func namespacedRawData(ns string) []byte {
	u := makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"})
	u.SetNamespace(ns)

	objData, _ := json.Marshal(u.Object)
	return objData
}

func unmatchedRawData() []byte {
	objData, err := json.Marshal(makeNamespacedResource(schema.GroupVersionKind{Group: "another", Kind: "thing"}, "foo", "bar").Object)
	if err != nil {
		panic(err)
	}

	return objData
}

func TestMatcher_Match(t *testing.T) {
	nsData, _ := json.Marshal(makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "foo").Object)

	ns := makeNamespace("my-ns", map[string]string{"ns": "label"})
	tests := []struct {
		name            string
		match           *match.Match
		cachedNs        *corev1.Namespace
		req             interface{}
		wantHandled     bool
		wantErr         error
		want            bool
		wantIsAdmission bool
	}{
		{
			name:            "nil",
			req:             nil,
			match:           nil,
			wantHandled:     false,
			wantErr:         nil,
			wantIsAdmission: false,
		},
		{
			name: "AdmissionRequest supported",
			req: admissionv1.AdmissionRequest{
				Object: runtime.RawExtension{Raw: matchedRawData()},
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            false,
			wantIsAdmission: false,
		},
		{
			name:            "unstructured.Unstructured supported",
			req:             makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            false,
			wantIsAdmission: false,
		},
		{
			name: "Raw object doesn't unmarshal",
			req: &AugmentedUnstructured{
				Namespace: makeNamespace("my-ns"),
				Object: unstructured.Unstructured{Object: map[string]interface{}{
					"key": "Some invalid json",
				}},
				Source: types.SourceTypeDefault,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         ErrRequestObject,
			want:            false,
			wantIsAdmission: false,
		},
		{
			name: "Match error",
			req: &AugmentedReview{
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: namespacedRawData("foo")},
				},
				IsAdmission: true,
			},
			match:           namespaceSelectorMatch(),
			wantHandled:     true,
			wantErr:         ErrMatching,
			want:            false,
			wantIsAdmission: true,
		},
		{
			name: "Success if Namespace not cached",
			req: &AugmentedReview{
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: nsData},
				},
				IsAdmission: true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            false,
			wantIsAdmission: true,
		},
		{
			name: "AugmentedReview is supported",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: matchedRawData()},
				},
				IsAdmission: true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            true,
			wantIsAdmission: true,
		},
		{
			name: "AugmentedUnstructured is supported",
			req: &AugmentedUnstructured{
				Namespace: ns,
				Object:    *makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            true,
			wantIsAdmission: false,
		},
		{
			name: "Both object and old object are matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: matchedRawData()},
					OldObject: runtime.RawExtension{Raw: matchedRawData()},
				},
				IsAdmission: true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            true,
			wantIsAdmission: true,
		},
		{
			name: "object is matched, old object is not matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: matchedRawData()},
					OldObject: runtime.RawExtension{Raw: unmatchedRawData()},
				},
				IsAdmission: true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            true,
			wantIsAdmission: true,
		},
		{
			name: "object is not matched, old object is matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: unmatchedRawData()},
					OldObject: runtime.RawExtension{Raw: matchedRawData()},
				},
				IsAdmission: true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            true,
			wantIsAdmission: true,
		},
		{
			name: "object is matched, old object is not matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: unmatchedRawData()},
					OldObject: runtime.RawExtension{Raw: unmatchedRawData()},
				},
				IsAdmission: true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            false,
			wantIsAdmission: true,
		},
		{
			name: "new object is not matched, old object is not specified",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: unmatchedRawData()},
				},
				IsAdmission: true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         nil,
			want:            false,
			wantIsAdmission: true,
		},
		{
			name:     "missing cached Namespace",
			cachedNs: nil,
			req: &AugmentedReview{
				Namespace: nil,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Namespace: "foo",
					Object:    runtime.RawExtension{Raw: namespacedRawData("foo")},
				},
				IsAdmission: true,
			},
			match: &match.Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"ns": "label"},
				},
			},
			wantHandled:     true,
			wantErr:         ErrMatching,
			want:            false,
			wantIsAdmission: true,
		},
		{
			name: "use cached Namespace no match",
			cachedNs: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
			},
			req: &AugmentedReview{
				Namespace: nil,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Namespace: "foo",
					Object:    runtime.RawExtension{Raw: namespacedRawData("foo")},
				},
				IsAdmission: true,
			},
			match: &match.Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"ns": "label"},
				},
			},
			wantHandled:     true,
			wantErr:         nil,
			want:            false,
			wantIsAdmission: true,
		},
		{
			name: "use cached Namespace match",
			cachedNs: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
					Labels: map[string]string{
						"ns": "label",
					},
				},
			},
			req: &AugmentedReview{
				Namespace: nil,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Namespace: "foo",
					Object:    runtime.RawExtension{Raw: namespacedRawData("foo")},
				},
				IsAdmission: true,
			},
			match: &match.Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"ns": "label"},
				},
			},
			wantHandled:     true,
			wantErr:         nil,
			want:            true,
			wantIsAdmission: true,
		},
		{
			name: "neither new or old object is specified",
			req: &AugmentedReview{
				Namespace:        ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{},
				IsAdmission:      true,
			},
			match:           fooMatch(),
			wantHandled:     true,
			wantErr:         ErrRequestObject,
			want:            false,
			wantIsAdmission: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &K8sValidationTarget{}
			m := &Matcher{
				match: tt.match,
				cache: newNsCache(),
			}

			if tt.cachedNs != nil {
				key := clusterScopedKey(corev1.SchemeGroupVersion.WithKind("Namespace"), tt.cachedNs.Name)
				m.cache.AddNamespace(toKey(key), tt.cachedNs)
			}

			handled, review, err := target.HandleReview(tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if review != nil {
				gkr, ok := review.(*gkReview)
				if !ok {
					t.Fatalf("test %v: HandleReview failed to return gkReview object", tt.name)
				}

				if gkr != nil && tt.wantIsAdmission != gkr.IsAdmissionRequest() {
					t.Fatalf("test %v: isAdmission = %v, wantIsAdmission %v", tt.name, gkr.IsAdmissionRequest(), tt.wantIsAdmission)
				}
			}

			if tt.wantHandled != handled {
				t.Fatalf("got handled = %t, want %t", handled, tt.want)
			}

			if !tt.wantHandled {
				return
			}

			got, err := m.Match(review)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Match() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("want %v matched, got %v", tt.want, got)
			}
		})
	}
}

func TestNamespaceCache(t *testing.T) {
	type wantNs struct {
		namespace   string
		ns          *corev1.Namespace
		shouldExist bool
	}

	tests := []struct {
		name     string
		addNs    []*unstructured.Unstructured
		removeNs []string
		checkNs  []wantNs
		wantErr  error
	}{
		{
			name:  "retrieving a namespace from empty cache returns nil",
			addNs: nil,
			checkNs: []wantNs{
				{
					namespace:   "my-ns1",
					ns:          makeNamespace("my-ns1", map[string]string{"ns1": "label"}),
					shouldExist: false,
				},
			},
			wantErr: nil,
		},
		{
			name: "retrieving a namespace that does not exist returns nil",
			addNs: []*unstructured.Unstructured{
				makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "my-ns1", map[string]string{"ns1": "label"}),
			},
			checkNs: []wantNs{
				{
					namespace:   "my-ns1",
					ns:          makeNamespace("my-ns1", map[string]string{"ns1": "label"}),
					shouldExist: true,
				},
				{
					namespace:   "my-ns2",
					ns:          makeNamespace("my-ns2", map[string]string{"ns2": "label"}),
					shouldExist: false,
				},
			},
			wantErr: nil,
		},
		{
			name: "retrieving an added namespace returns the namespace",
			addNs: []*unstructured.Unstructured{
				makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "my-ns1", map[string]string{"ns1": "label"}),
				makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "my-ns2", map[string]string{"ns2": "label"}),
			},
			checkNs: []wantNs{
				{
					namespace:   "my-ns1",
					ns:          makeNamespace("my-ns1", map[string]string{"ns1": "label"}),
					shouldExist: true,
				},
				{
					namespace:   "my-ns2",
					ns:          makeNamespace("my-ns2", map[string]string{"ns2": "label"}),
					shouldExist: true,
				},
			},
			wantErr: nil,
		},
		{
			name: "adding an invalid Namespace does not work",
			addNs: []*unstructured.Unstructured{{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"spec":       3.0,
				},
			}},
			checkNs: []wantNs{},
			wantErr: ErrCachingType,
		},
		{
			name: "adding a non-namespace type returns error",
			addNs: []*unstructured.Unstructured{
				fooConstraint(),
				makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "my-ns2", map[string]string{"ns2": "label"}),
			},
			checkNs: []wantNs{
				{
					namespace:   "my-ns2",
					ns:          makeNamespace("my-ns2", map[string]string{"ns2": "label"}),
					shouldExist: true,
				},
			},
			wantErr: ErrCachingType,
		},
		{
			name: "removing a namespace returns nil when retrieving",
			addNs: []*unstructured.Unstructured{
				makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "my-ns1", map[string]string{"ns1": "label"}),
				makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, "my-ns2", map[string]string{"ns2": "label"}),
			},
			removeNs: []string{"my-ns1"},
			checkNs: []wantNs{
				{
					namespace:   "my-ns1",
					ns:          makeNamespace("my-ns1", map[string]string{"ns1": "label"}),
					shouldExist: false,
				},
				{
					namespace:   "my-ns2",
					ns:          makeNamespace("my-ns2", map[string]string{"ns2": "label"}),
					shouldExist: true,
				},
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &K8sValidationTarget{}

			for _, ns := range tt.addNs {
				_, key, _, err := target.ProcessData(ns)
				if err != nil {
					t.Fatal(err)
				}

				err = target.GetCache().Add(key, ns.Object)
				if err != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("Add() error = %v, wantErr = %v", err, tt.wantErr)
				}
			}

			for _, name := range tt.removeNs {
				ns := makeResource(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, name)
				_, key, _, err := target.ProcessData(ns)
				if err != nil {
					t.Fatal(err)
				}

				target.GetCache().Remove(key)
			}

			wantCount := 0
			gotCount := len(target.cache.cache)
			for _, want := range tt.checkNs {
				if want.shouldExist {
					wantCount++
				}

				got := target.cache.GetNamespace(want.namespace)

				if !want.shouldExist && got == nil {
					continue
				}

				if diff := cmp.Diff(got, want.ns); diff != "" {
					t.Errorf("+got -want:\n%s", diff)
				}
			}

			if gotCount != wantCount {
				t.Fatalf("got %d members in cache, want %d", gotCount, wantCount)
			}
		})
	}
}

func newNsCache() *nsCache {
	return &nsCache{
		cache: make(map[string]*corev1.Namespace),
	}
}
