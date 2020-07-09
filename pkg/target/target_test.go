package target

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
