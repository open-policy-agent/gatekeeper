package target

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestFrameworkInjection(t *testing.T) {
	target := &K8sValidationTarget{}
	driver := local.New(local.Tracing(true))
	backend, err := client.NewBackend(client.Driver(driver))
	if err != nil {
		t.Fatalf("Could not initialize backend: %s", err)
	}
	_, err = backend.NewClient(client.Targets(target))
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
			ErrorExpected: false,
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
			ErrorExpected: false,
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

func TestHandleViolation(t *testing.T) {
	tc := []struct {
		Name          string
		Review        string
		ErrorExpected bool
		ExpectedObj   string
	}{
		{
			Name: "Valid Review",
			Review: `
{
	"kind": {
		"group": "myGroup",
		"version": "v1",
		"kind": "MyKind"
	},
	"name": "somename",
	"operation": "CREATE",
	"object": {
		"metadata": {"name": "somename"},
		"spec": {"value": "yep"}
	}
}
`,
			ExpectedObj: `
{
	"apiVersion": "myGroup/v1",
	"kind": "MyKind",
	"metadata": {"name": "somename"},
	"spec": {"value": "yep"}
}
`,
		},
		{
			Name: "Valid Review (No Group)",
			Review: `
{
	"kind": {
		"group": "",
		"version": "v1",
		"kind": "MyKind"
	},
	"name": "somename",
	"operation": "CREATE",
	"object": {
		"metadata": {"name": "somename"},
		"spec": {"value": "yep"}
	}
}
`,
			ExpectedObj: `
{
	"apiVersion": "v1",
	"kind": "MyKind",
	"metadata": {"name": "somename"},
	"spec": {"value": "yep"}
}
`,
		},
		{
			Name:          "No Review",
			Review:        `["list is wrong"]`,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			r := &types.Result{}
			var i interface{}
			err := json.Unmarshal([]byte(tt.Review), &i)
			if err != nil {
				t.Fatalf("Error parsing result: %s", err)
			}
			r.Review = i
			h := &K8sValidationTarget{}
			err = h.HandleViolation(r)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %s; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
			if tt.ExpectedObj != "" {
				expected := &unstructured.Unstructured{}
				err = json.Unmarshal([]byte(tt.ExpectedObj), expected)
				if err != nil {
					t.Fatalf("Error parsing expected obj: %s", err)
				}
				if !reflect.DeepEqual(r.Resource, expected) {
					t.Errorf("result.Resource = %s; wanted %s", spew.Sdump(r.Resource), spew.Sdump(expected))
				}
			}
		})
	}
}

func TestProcessData(t *testing.T) {
	tc := []struct {
		Name          string
		JSON          string
		ErrorExpected bool
		ExpectedPath  string
	}{
		{
			Name:         "Cluster Object",
			JSON:         `{"apiVersion": "v1beta1", "kind": "Rock", "metadata": {"name": "myrock"}}`,
			ExpectedPath: "cluster/v1beta1/Rock/myrock",
		},
		{
			Name:         "Namespace Object",
			JSON:         `{"apiVersion": "v1beta1", "kind": "Rock", "metadata": {"name": "myrock", "namespace": "foo"}}`,
			ExpectedPath: "namespace/foo/v1beta1/Rock/myrock",
		},
		{
			Name:         "Grouped Object",
			JSON:         `{"apiVersion": "mygroup/v1beta1", "kind": "Rock", "metadata": {"name": "myrock"}}`,
			ExpectedPath: "cluster/mygroup%2Fv1beta1/Rock/myrock",
		},
		{
			Name:          "No Version",
			JSON:          `{"kind": "Rock", "metadata": {"name": "myrock", "namespace": "foo"}}`,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			h := &K8sValidationTarget{}
			o := &unstructured.Unstructured{}
			err := json.Unmarshal([]byte(tt.JSON), o)
			if err != nil {
				t.Fatalf("Error parsing JSON: %s", err)
			}
			handled, path, data, err := h.ProcessData(o)
			if !handled {
				t.Errorf("handled = false; want true")
			}
			if !tt.ErrorExpected {
				if path != tt.ExpectedPath {
					t.Errorf("path = %s; want %s", path, tt.ExpectedPath)
				}
				if !reflect.DeepEqual(data, o.Object) {
					t.Errorf(cmp.Diff(data, o.Object))
				}
				if err != nil {
					t.Errorf("err = %s; want nil", err)
				}
			} else {
				if path != "" {
					t.Errorf("path = %s; want empty string", path)
				}
				if data != nil {
					t.Errorf("data = %v; want nil", spew.Sdump(data))
				}
				if err == nil {
					t.Errorf("err = nil; want non-nil")
				}
			}
		})
	}
}

func fooMatch() *match.Match {
	return &match.Match{
		Kinds: []match.Kinds{
			{
				Kinds:     []string{"Thing"},
				APIGroups: []string{"some"},
			},
		},
		Scope:              "Namespaced",
		Namespaces:         []util.Wildcard{"my-ns"},
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
	)
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
			wantErr:    ErrCreatingMather,
		},
		{
			name:       "constraint with match fields",
			constraint: fooConstraint(),
			want: &Matcher{
				fooMatch(),
			},
		},
		{
			name: "mutator with match fields",
			constraint: &unstructured.Unstructured{
				Object: unstructAssign,
			},
			want: &Matcher{
				fooMatch(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &K8sValidationTarget{}
			got, err := h.ToMatcher(tt.constraint)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ToMatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(got, tt.want, cmp.AllowUnexported(Matcher{})); diff != "" {
				t.Errorf("ToMatcher() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func matchedRawData() []byte {
	objData, _ := json.Marshal(makeResource("some", "Thing", map[string]string{"obj": "label"}).Object)
	return objData
}

func unmatchedRawData() []byte {
	objData, _ := json.Marshal(makeResource("another", "thing").Object)
	return objData
}

func TestMatcher_Match(t *testing.T) {
	ns := makeNamespace("my-ns", map[string]string{"ns": "label"})
	tests := []struct {
		name    string
		match   *match.Match
		req     interface{}
		wantErr error
		want    bool
	}{
		{
			name:    "AdmissionRequest not supported",
			req:     admissionv1.AdmissionRequest{},
			wantErr: ErrReviewFormat,
		},
		{
			name:    "unstructured.Unstructured not supported",
			req:     makeResource("some", "Thing"),
			wantErr: ErrReviewFormat,
		},
		{
			name: "Raw object doesn't unmarshal",
			req: &AugmentedUnstructured{
				Namespace: makeNamespace("my-ns"),
				Object: unstructured.Unstructured{Object: map[string]interface{}{
					"key": "Some invalid json",
				}},
			},
			wantErr: ErrRequestObject,
		},
		{
			name: "AugmentedReview is supported",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: matchedRawData()},
				},
			},
			match: fooMatch(),
			want:  true,
		},
		{
			name: "AugmentedUnstructured is supported",
			req: &AugmentedUnstructured{
				Namespace: ns,
				Object:    *makeResource("some", "Thing", map[string]string{"obj": "label"}),
			},
			match: fooMatch(),
			want:  true,
		},
		{
			name: "Both object and old object are matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: matchedRawData()},
					OldObject: runtime.RawExtension{Raw: matchedRawData()},
				},
			},
			match: fooMatch(),
			want:  true,
		},
		{
			name: "object is matched, old object is not matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: matchedRawData()},
					OldObject: runtime.RawExtension{Raw: unmatchedRawData()},
				},
			},
			match: fooMatch(),
			want:  true,
		},
		{
			name: "object is not matched, old object is matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: unmatchedRawData()},
					OldObject: runtime.RawExtension{Raw: matchedRawData()},
				},
			},
			match: fooMatch(),
			want:  true,
		},
		{
			name: "object is matched, old object is not matched",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object:    runtime.RawExtension{Raw: unmatchedRawData()},
					OldObject: runtime.RawExtension{Raw: unmatchedRawData()},
				},
			},
			match: fooMatch(),
			want:  false,
		},
		{
			name: "new object is not matched, old object is not specified",
			req: &AugmentedReview{
				Namespace: ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{Raw: unmatchedRawData()},
				},
			},
			match: fooMatch(),
			want:  false,
		},
		{
			name: "neither new or old object is specified",
			req: &AugmentedReview{
				Namespace:        ns,
				AdmissionRequest: &admissionv1.AdmissionRequest{},
			},
			match:   fooMatch(),
			wantErr: ErrRequestObject,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &K8sValidationTarget{}
			m := &Matcher{
				match: tt.match,
			}
			handled, review, err := target.HandleReview(tt.req)
			if !handled || err != nil {
				t.Fatalf("failed to handle review %v", err)
			}
			got, err := m.Match(review)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Match() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("want %v matched, got %v", tt.want, got)
			}
		})
	}
}
