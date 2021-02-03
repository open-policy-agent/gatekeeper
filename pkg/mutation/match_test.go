package mutation_test

import (
	"encoding/json"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	mutationsv1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMatch(t *testing.T) {
	table := []struct {
		tname       string
		toMatch     *unstructured.Unstructured
		match       mutationsv1.Match
		namespace   *corev1.Namespace
		shouldMatch bool
	}{
		{
			tname:   "match kind with *",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"*"},
						APIGroups: []string{"*"},
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: true,
		},
		{
			tname:   "match group and no kinds specified should match",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{"*"},
					},
					{
						APIGroups: []string{"*"},
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: true,
		},
		{
			tname:   "match kind and no group specified should match",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds: []string{"kind", "neithermatching"},
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: true,
		},
		{
			tname:   "match kind and group explicit",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{"*"},
					},
					{
						Kinds:     []string{"notmatching", "kind"},
						APIGroups: []string{"*"},
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: true,
		},
		{
			tname:   "kind group don't matches",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{"*"},
					},
					{
						Kinds:     []string{"notmatching", "kind"},
						APIGroups: []string{"*"},
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: true,
		},
		{
			tname:   "kind group don't matches",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{"*"},
					},
					{
						Kinds:     []string{"notmatching", "kind"},
						APIGroups: []string{"notmatchinggroup"},
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: false,
		},
		{
			tname:   "namespace matches",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Namespaces: []string{"nonmatching", "namespace"},
			},
			namespace:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "namespace"}},
			shouldMatch: true,
		},
		{
			tname:   "namespace is not in the matches list",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Namespaces: []string{"nonmatching", "notmatchingeither"},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: false,
		},
		{
			tname:   "namespace fails if clusterscoped",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Namespaces: []string{"nonmatching", "namespace"},
				Scope:      apiextensionsv1beta1.ClusterScoped,
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: false,
		},
		{
			tname:   "namespace is excluded",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"kind"},
						APIGroups: []string{"group"},
					},
				},
				Namespaces:         []string{"nonmatching", "namespace"},
				ExcludedNamespaces: []string{"namespace"},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: false,
		},
		{
			tname:   "namespace scoped fails if cluster scoped",
			toMatch: makeObject("kind", "group", "", "name"),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"kind"},
						APIGroups: []string{"group"},
					},
				},
				Scope: apiextensionsv1beta1.NamespaceScoped,
			},
			namespace:   nil,
			shouldMatch: false,
		},
		{
			tname: "label selector",
			toMatch: makeObject("kind", "group", "", "name", func(o *unstructured.Unstructured) {
				meta, _ := meta.Accessor(o)
				meta.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"kind"},
						APIGroups: []string{"group"},
					},
				},
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: true,
		},
		{
			tname: "label selector not matching",
			toMatch: makeObject("kind", "group", "", "name", func(o *unstructured.Unstructured) {
				meta, _ := meta.Accessor(o)
				meta.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"kind"},
						APIGroups: []string{"group"},
					},
				},
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname":        "labelvalue",
						"labelnotmatching": "foo",
					},
				},
			},
			namespace:   &corev1.Namespace{},
			shouldMatch: false,
		},
		{
			tname:   "namespace selector",
			toMatch: makeObject("kind", "group", "", "name"),
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
					Labels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"kind"},
						APIGroups: []string{"group"},
					},
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			shouldMatch: true,
		},
		{
			tname:   "namespace selector not matching",
			toMatch: makeObject("kind", "group", "foo", "name"),
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
					Labels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"kind"},
						APIGroups: []string{"group"},
					},
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
						"foo":       "bar",
					},
				},
			},
			shouldMatch: false,
		},
		{
			tname:     "namespace selector not matching, but cluster scoped",
			toMatch:   makeObject("kind", "group", "", "name"),
			namespace: nil,
			match: mutationsv1.Match{
				Kinds: []mutationsv1.Kinds{
					{
						Kinds:     []string{"kind"},
						APIGroups: []string{"group"},
					},
				},
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
						"foo":       "bar",
					},
				},
			},
			shouldMatch: true,
		},
		{
			tname: "namespace selector is applied to the object, if the object is a namespace",
			toMatch: makeNamespace("namespace", func(o *unstructured.Unstructured) {
				meta, _ := meta.Accessor(o)
				meta.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			namespace: nil,
			match: mutationsv1.Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			shouldMatch: true,
		},
		{
			tname: "namespace selector is applied to the namespace, and does not match",
			toMatch: makeNamespace("namespace", func(o *unstructured.Unstructured) {
				meta, _ := meta.Accessor(o)
				meta.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			namespace: nil,
			match: mutationsv1.Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "badvalue",
					},
				},
			},
			shouldMatch: false,
		},
	}
	for _, tc := range table {
		t.Run(tc.tname, func(t *testing.T) {
			ns := tc.namespace
			nsgk := schema.GroupKind{Group: "", Kind: "Namespace"}
			if tc.toMatch.GetObjectKind().GroupVersionKind().GroupKind() == nsgk {
				b, err := json.Marshal(tc.toMatch.Object)
				if err != nil {
					t.Fatal(err)
				}
				ns = &corev1.Namespace{}
				if err := json.Unmarshal(b, ns); err != nil {
					t.Fatal(err)
				}
			}
			// namespace is not populated in the object metadata for mutation requests
			tc.toMatch.SetNamespace("")
			matches, err := mutation.Matches(tc.match, tc.toMatch, ns)
			if err != nil {
				t.Error("Match failed for ", tc.tname)
			}
			if matches != tc.shouldMatch {
				t.Errorf("%s: expecting match to be %v, was %v", tc.tname, tc.shouldMatch, matches)
			}
		})
	}
}

func makeObject(kind, group, namespace, name string, options ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	config := &configv1alpha1.Config{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	gvk := schema.GroupVersionKind{
		Kind:    kind,
		Group:   group,
		Version: "v1",
	}
	config.APIVersion, config.Kind = gvk.ToAPIVersionAndKind()
	unstruct, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(config)

	res := &unstructured.Unstructured{Object: unstruct}
	for _, o := range options {
		o(res)
	}
	return res
}

func makeNamespace(name string, options ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	unstruct, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(namespace)

	res := &unstructured.Unstructured{Object: unstruct}
	for _, o := range options {
		o(res)
	}
	return res
}

func TestApplyTo(t *testing.T) {
	table := []struct {
		tname       string
		toMatch     *unstructured.Unstructured
		applyTo     []mutationsv1.ApplyTo
		shouldApply bool
	}{
		{
			tname:   "one item, applies",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			applyTo: []mutationsv1.ApplyTo{
				{
					Groups:   []string{"group"},
					Kinds:    []string{"kind"},
					Versions: []string{"v1"},
				},
			},
			shouldApply: true,
		},
		{
			tname:   "one item, many columns",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			applyTo: []mutationsv1.ApplyTo{
				{
					Groups:   []string{"aa", "bb", "group"},
					Kinds:    []string{"aa", "bb", "kind"},
					Versions: []string{"aa", "bb", "v1"},
				},
			},
			shouldApply: true,
		},
		{
			tname:   "first don't match, second does",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			applyTo: []mutationsv1.ApplyTo{
				{
					Groups:   []string{"group"},
					Kinds:    []string{"not matching"},
					Versions: []string{"v1"},
				},
				{
					Groups:   []string{"group"},
					Kinds:    []string{"kind"},
					Versions: []string{"v1"},
				},
			},
			shouldApply: true,
		},
		{
			tname:   "no one is matching",
			toMatch: makeObject("kind", "group", "namespace", "name"),
			applyTo: []mutationsv1.ApplyTo{
				{
					Groups:   []string{"group"},
					Kinds:    []string{"not matching"},
					Versions: []string{"v1"},
				},
				{
					Groups:   []string{"neither", "neither1"},
					Kinds:    []string{"kind"},
					Versions: []string{"v1"},
				},
			},
			shouldApply: false,
		},
	}
	for _, tc := range table {
		t.Run(tc.tname, func(t *testing.T) {
			appliesTo := mutation.AppliesTo(tc.applyTo, tc.toMatch)
			if appliesTo != tc.shouldApply {
				t.Errorf("%s: expecting match to be %v, was %v", tc.tname, tc.shouldApply, appliesTo)
			}
		})
	}
}
