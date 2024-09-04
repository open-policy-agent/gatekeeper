package target

import (
	"context"
	"encoding/json"
	"testing"

	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	api "github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
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

func setScope(scope string) buildArg {
	return func(obj *unstructured.Unstructured) {
		if err := unstructured.SetNestedField(obj.Object, scope, "spec", "match", "scope"); err != nil {
			panic(err)
		}
	}
}

func setSource(source string) buildArg {
	return func(obj *unstructured.Unstructured) {
		if err := unstructured.SetNestedField(obj.Object, source, "spec", "match", "source"); err != nil {
			panic(err)
		}
	}
}

func setName(name string) buildArg {
	return func(obj *unstructured.Unstructured) {
		if err := unstructured.SetNestedField(obj.Object, name, "spec", "match", "name"); err != nil {
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

func makeResource(gvk schema.GroupVersionKind, name string, labels ...map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{},
	}
	u.SetName(name)
	u.SetGroupVersionKind(gvk)

	if len(labels) > 0 {
		u.SetLabels(labels[0])
	}
	return u
}

func makeNamespacedResource(gvk schema.GroupVersionKind, namespace, name string, labels ...map[string]string) *unstructured.Unstructured {
	u := makeResource(gvk, name, labels...)
	u.SetNamespace(namespace)
	return u
}

func makeNamespace(name string, labels ...map[string]string) *corev1.Namespace {
	ns := &corev1.Namespace{}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))
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
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(),
			allowed:    false,
		},
		{
			name:       "match namespace",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setNamespaceName("my-ns")),
			allowed:    false,
		},
		{
			name:       "no match namespace",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setNamespaceName("not-my-ns")),
			allowed:    true,
		},
		{
			name:       "match excludedNamespaces",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setExcludedNamespaceName("my-ns")),
			allowed:    true,
		},
		{
			name:       "no match excludedNamespaces",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setExcludedNamespaceName("not-my-ns")),
			allowed:    false,
		},
		{
			name:       "match labelselector",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"a": "label"}),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setLabelSelector("a", "label")),
			allowed:    false,
		},
		{
			name:       "no match labelselector",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"a": "label"}),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setLabelSelector("different", "label")),
			allowed:    true,
		},
		{
			name:       "match nsselector",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns", map[string]string{"a": "label"}),
			constraint: makeConstraint(setNamespaceSelector("a", "label")),
			allowed:    false,
		},
		{
			name:       "no match nsselector",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns", map[string]string{"a": "label"}),
			constraint: makeConstraint(setNamespaceSelector("different", "label")),
			allowed:    true,
		},
		{
			name:       "match kinds",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setKinds([]string{"some"}, []string{"Thing"})),
			allowed:    false,
		},
		{
			name:       "no match kinds",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setKinds([]string{"different"}, []string{"Thing"})),
			allowed:    true,
		},
		{
			name:       "match name",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setName("foo")),
			allowed:    false,
		},
		{
			name:       "no match name",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setName("other-name")),
			allowed:    true,
		},
		{
			name:       "match name wildcard",
			obj:        makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "test-resource"),
			ns:         makeNamespace("my-ns"),
			constraint: makeConstraint(setName("test-*")),
			allowed:    false,
		},
		{
			name: "match everything",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
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
			name: "match everything with scope as wildcard",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setScope("*"),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: false,
		},
		{
			name: "match everything with scope as namespaced",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setScope("Namespaced"),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: false,
		},
		{
			name: "match everything with scope as cluster",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setScope("Cluster"),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: true,
		},
		{
			name: "match everything but kind",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
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
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
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
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
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
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			ns:   makeNamespace("my-ns", map[string]string{"ns": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "different-label"),
			),
			allowed: true,
		},
		{
			name: "match everything cluster scoped",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: false,
		},
		{
			name: "match everything cluster scoped wildcard as scope",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setScope("*"),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: false,
		},
		{
			name: "do not match everything cluster scoped namespaced as scope",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setScope("Namespaced"),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: true,
		},
		{
			name: "match everything cluster scoped with cluster as scope",
			obj:  makeResource(schema.GroupVersionKind{Group: "some", Kind: "Thing"}, "foo", map[string]string{"obj": "label"}),
			constraint: makeConstraint(
				setKinds([]string{"some"}, []string{"Thing"}),
				setScope("Cluster"),
				setNamespaceName("my-ns"),
				setLabelSelector("obj", "label"),
				setNamespaceSelector("ns", "label"),
			),
			allowed: false,
		},
	}

	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatalf("could not initialize scheme: %s", err)
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			target := &K8sValidationTarget{}
			driver, err := rego.New(rego.Tracing(true))
			if err != nil {
				t.Fatalf("unable to set up Driver: %v", err)
			}

			c, err := constraintclient.NewClient(constraintclient.Targets(target), constraintclient.Driver(driver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
			if err != nil {
				t.Fatalf("unable to set up OPA client: %s", err)
			}

			versionedTmpl := &templatesv1beta1.ConstraintTemplate{}
			if err := yaml.Unmarshal([]byte(testTemplate), versionedTmpl); err != nil {
				t.Fatalf("unable to unmarshal template: %s", err)
			}
			tmpl := &templates.ConstraintTemplate{}
			if err := scheme.Convert(versionedTmpl, tmpl, nil); err != nil {
				t.Fatalf("could not convert template: %s", err)
			}
			ctx := context.Background()
			if _, err := c.AddTemplate(ctx, tmpl); err != nil {
				t.Fatalf("unable to add template: %s", err)
			}
			if _, err := c.AddConstraint(context.Background(), tc.constraint); err != nil {
				t.Fatalf("unable to add constraint: %s", err)
			}

			objData, err := json.Marshal(tc.obj.Object)
			if err != nil {
				t.Fatalf("unable to marshal obj: %s", err)
			}
			req := &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   tc.obj.GroupVersionKind().Group,
					Version: tc.obj.GroupVersionKind().Version,
					Kind:    tc.obj.GroupVersionKind().Kind,
				},
				Object: runtime.RawExtension{
					Raw: objData,
				},
			}

			if tc.ns != nil {
				req.Namespace = tc.ns.Name
			}

			fullReq := &AugmentedReview{Namespace: tc.ns, AdmissionRequest: req}
			res, err := c.Review(ctx, fullReq, reviews.EnforcementPoint(util.AuditEnforcementPoint), reviews.Tracing(true))
			if err != nil {
				t.Errorf("Error reviewing request: %s", err)
			}
			if (len(res.Results()) == 0) != tc.allowed {
				dump, err := c.Dump(ctx)
				if err != nil {
					t.Logf("error dumping: %s", err)
				}
				t.Fatalf("allowed = %v, expected %v:\n%s\n\n%s", !tc.allowed, tc.allowed, res.TraceDump(), dump)
			}

			// also test oldObject
			req2 := &admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   tc.obj.GroupVersionKind().Group,
					Version: tc.obj.GroupVersionKind().Version,
					Kind:    tc.obj.GroupVersionKind().Kind,
				},
				OldObject: runtime.RawExtension{
					Raw: objData,
				},
			}

			if tc.ns != nil {
				req2.Namespace = tc.ns.Name
			}

			fullReq2 := &AugmentedReview{Namespace: tc.ns, AdmissionRequest: req2}
			res2, err := c.Review(ctx, fullReq2, reviews.EnforcementPoint(util.AuditEnforcementPoint), reviews.Tracing(true))
			if err != nil {
				t.Errorf("Error reviewing OldObject request: %s", err)
			}
			if (len(res2.Results()) == 0) != tc.allowed {
				dump, err := c.Dump(ctx)
				if err != nil {
					t.Logf("error dumping: %s", err)
				}
				t.Errorf("allowed = %v, expected %v:\n%s\n\n%s", !tc.allowed, tc.allowed, res2.TraceDump(), dump)
			}

			fullReq3 := &AugmentedUnstructured{Namespace: tc.ns, Object: *tc.obj}
			res3, err := c.Review(ctx, fullReq3, reviews.EnforcementPoint(util.AuditEnforcementPoint), reviews.Tracing(true))
			if err != nil {
				t.Errorf("Error reviewing AugmentedUnstructured request: %s", err)
			}
			if (len(res3.Results()) == 0) != tc.allowed {
				dump, err := c.Dump(ctx)
				if err != nil {
					t.Logf("error dumping: %s", err)
				}
				t.Errorf("allowed = %v, expected %v:\n%s\n\n%s", !tc.allowed, tc.allowed, res3.TraceDump(), dump)
			}
		})
	}
}
