package target

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	testTemplate = `
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: denyall
spec:
  crd:
    spec:
      names:
        kind: DenyAll
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package denyall


        violation[{"msg": msg}] {
          msg := "denyall constraint installed"
        }
`
)

type buildArg func(*unstructured.Unstructured)

func setKinds(groups, kinds []string) buildArg {
	return func(obj *unstructured.Unstructured) {
		var iKinds []interface{}
		for _, v := range kinds {
			iKinds = append(iKinds, v)
		}
		var iGroups []interface{}
		for _, v := range groups {
			iGroups = append(iGroups, v)
		}
		kindMatch := map[string]interface{}{
			"apiGroups": iGroups,
			"kinds":     iKinds,
		}
		if err := unstructured.SetNestedSlice(obj.Object, []interface{}{kindMatch}, "spec", "match", "kinds"); err != nil {
			panic(err)
		}
	}
}

func setLabelSelector(key, value string) buildArg {
	return func(obj *unstructured.Unstructured) {
		if err := unstructured.SetNestedField(obj.Object, value, "spec", "match", "labelSelector", "matchLabels", key); err != nil {
			panic(err)
		}
	}
}

func setNamespaceSelector(key, value string) buildArg {
	return func(obj *unstructured.Unstructured) {
		if err := unstructured.SetNestedField(obj.Object, value, "spec", "match", "namespaceSelector", "matchLabels", key); err != nil {
			panic(err)
		}
	}
}

func setNamespaceName(name string) buildArg {
	return func(obj *unstructured.Unstructured) {
		if err := unstructured.SetNestedSlice(obj.Object, []interface{}{name}, "spec", "match", "namespaces"); err != nil {
			panic(err)
		}
	}
}

func setExcludedNamespaceName(name string) buildArg {
	return func(obj *unstructured.Unstructured) {
		if err := unstructured.SetNestedSlice(obj.Object, []interface{}{name}, "spec", "match", "excludedNamespaces"); err != nil {
			panic(err)
		}
	}
}

func makeConstraint(o ...buildArg) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetName("my-constraint")
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "DenyAll"})
	for _, fn := range o {
		fn(u)
	}
	return u
}

func makeResource(group, kind string, labels ...map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: "v1", Kind: kind})
	if len(labels) > 0 {
		u.SetLabels(labels[0])
	}
	return u
}

func makeNamespace(name string, labels ...map[string]string) *corev1.Namespace {
	ns := &corev1.Namespace{}
	ns.Name = name
	if len(labels) > 0 {
		ns.Labels = labels[0]
	}
	return ns
}

func TestConstraintEnforcement(t *testing.T) {
	tcs := []struct {
		name       string
		obj        *unstructured.Unstructured
		ns         *corev1.Namespace
		constraint *unstructured.Unstructured
		allowed    bool
	}{
		{
			name:       "match deny all",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(),
			allowed:    false,
		},
		{
			name:       "match namespace",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setNamespaceName("my-ns")),
			allowed:    false,
		},
		{
			name:       "no match namespace",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setNamespaceName("not-my-ns")),
			allowed:    true,
		},
		{
			name:       "match excludedNamespaces",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setExcludedNamespaceName("my-ns")),
			allowed:    true,
		},
		{
			name:       "no match excludedNamespaces",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setExcludedNamespaceName("not-my-ns")),
			allowed:    false,
		},
		{
			name:       "match labelselector",
			obj:        makeResource("some", "Thing", map[string]string{"a": "label"}),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setLabelSelector("a", "label")),
			allowed:    false,
		},
		{
			name:       "no match labelselector",
			obj:        makeResource("some", "Thing", map[string]string{"a": "label"}),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setLabelSelector("different", "label")),
			allowed:    true,
		},
		{
			name:       "match nsselector",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns", map[string]string{"a": "label"}),
			constraint: makeConstraint(setNamespaceSelector("a", "label")),
			allowed:    false,
		},
		{
			name:       "no match nsselector",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns", map[string]string{"a": "label"}),
			constraint: makeConstraint(setNamespaceSelector("different", "label")),
			allowed:    true,
		},
		{
			name:       "match kinds",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setKinds([]string{"some"}, []string{"Thing"})),
			allowed:    false,
		},
		{
			name:       "no match kinds",
			obj:        makeResource("some", "Thing"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setKinds([]string{"different"}, []string{"Thing"})),
			allowed:    true,
		},
		{
			name: "match everything",
			obj:  makeResource("some", "Thing", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: false,
		},
		{
			name: "match everything but kind",
			obj:  makeResource("some", "Thing", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"different"}, []string{"Thing"}),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: true,
		},
		{
			name: "match everything but namespace",
			obj:  makeResource("some", "Thing", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setNamespaceName("different-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: true,
		},
		{
			name: "match everything but labelselector",
			obj:  makeResource("some", "Thing", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "different-label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: true,
		},
		{
			name: "match everything but nsselector",
			obj:  makeResource("some", "Thing", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "different-label"),
			),
			allowed: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			target := &K8sValidationTarget{}
			driver := local.New(local.Tracing(true))
			backend, err := client.NewBackend(client.Driver(driver))
			if err != nil {
				t.Fatalf("Could not initialize backend: %s", err)
			}
			c, err := backend.NewClient(client.Targets(target))
			if err != nil {
				t.Fatalf("unable to set up OPA client: %s", err)
			}

			tmpl := &templates.ConstraintTemplate{}
			if err := yaml.Unmarshal([]byte(testTemplate), tmpl); err != nil {
				t.Fatalf("unable to unmarshal template: %s", err)
			}
			if _, err := c.AddTemplate(context.Background(), tmpl); err != nil {
				t.Fatalf("unable to add template: %s", err)
			}
			if _, err := c.AddConstraint(context.Background(), tc.constraint); err != nil {
				t.Fatalf("unable to add constraint: %s", err)
			}

			objData, err := json.Marshal(tc.obj.Object)
			if err != nil {
				t.Fatalf("unable to marshal obj: %s", err)
			}
			req := &admissionv1beta1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   tc.obj.GroupVersionKind().Group,
					Version: tc.obj.GroupVersionKind().Version,
					Kind:    tc.obj.GroupVersionKind().Kind,
				},
				Object: runtime.RawExtension{
					Raw: objData,
				},
				Namespace: tc.ns.Name,
			}
			fullReq := &AugmentedReview{Namespace: tc.ns, AdmissionRequest: req}
			res, err := c.Review(context.Background(), fullReq, client.Tracing(true))
			if err != nil {
				t.Errorf("Error reviewing request: %s", err)
			}
			if (len(res.Results()) == 0) != tc.allowed {
				dump, err := c.Dump(context.Background())
				if err != nil {
					t.Logf("error dumping: %s", err)
				}
				t.Errorf("allowed = %v, expected %v:\n%s\n\n%s", !tc.allowed, tc.allowed, res.TraceDump(), dump)
			}

			//also test oldObject
			req2 := &admissionv1beta1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   tc.obj.GroupVersionKind().Group,
					Version: tc.obj.GroupVersionKind().Version,
					Kind:    tc.obj.GroupVersionKind().Kind,
				},
				OldObject: runtime.RawExtension{
					Raw: objData,
				},
				Namespace: tc.ns.Name,
			}
			fullReq2 := &AugmentedReview{Namespace: tc.ns, AdmissionRequest: req2}
			res2, err := c.Review(context.Background(), fullReq2, client.Tracing(true))
			if err != nil {
				t.Errorf("Error reviewing OldObject request: %s", err)
			}
			if (len(res2.Results()) == 0) != tc.allowed {
				dump, err := c.Dump(context.Background())
				if err != nil {
					t.Logf("error dumping: %s", err)
				}
				t.Errorf("allowed = %v, expected %v:\n%s\n\n%s", !tc.allowed, tc.allowed, res2.TraceDump(), dump)
			}

			fullReq3 := &AugmentedUnstructured{Namespace: tc.ns, Object: *tc.obj}
			res3, err := c.Review(context.Background(), fullReq3, client.Tracing(true))
			if err != nil {
				t.Errorf("Error reviewing AugmentedUnstructured request: %s", err)
			}
			if (len(res3.Results()) == 0) != tc.allowed {
				dump, err := c.Dump(context.Background())
				if err != nil {
					t.Logf("error dumping: %s", err)
				}
				t.Errorf("allowed = %v, expected %v:\n%s\n\n%s", !tc.allowed, tc.allowed, res3.TraceDump(), dump)
			}
		})
	}
}
