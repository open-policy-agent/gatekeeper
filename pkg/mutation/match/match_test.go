package match

import (
	"errors"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMatch(t *testing.T) {
	table := []struct {
		name      string
		object    *unstructured.Unstructured
		matcher   Match
		namespace *corev1.Namespace
		wantMatch bool
		wantErr   error
	}{
		{
			// Demonstrates why we need to use reflect in Matches to determine if obj
			// is nil.
			name:   "nil object",
			object: nil,
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{},
			},
			wantMatch: false,
			wantErr:   ErrMatch,
		},
		{
			name:   "match empty group kinds",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{},
						APIGroups: []string{},
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "match empty kinds",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{},
						APIGroups: []string{Wildcard},
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "don't match empty kinds in other group",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{},
						APIGroups: []string{"rbac"},
					},
				},
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "match kind with wildcard",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{Wildcard},
						APIGroups: []string{Wildcard},
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "match group and no kinds specified should match",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{Wildcard},
					},
					{
						APIGroups: []string{Wildcard},
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "match kind and no group specified should match",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds: []string{"kind", "neithermatching"},
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "match kind and group explicit",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{Wildcard},
					},
					{
						Kinds:     []string{"notmatching", "kind"},
						APIGroups: []string{Wildcard},
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "kind group doesn't match",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{Wildcard},
					},
					{
						Kinds:     []string{"notmatching", "kind"},
						APIGroups: []string{Wildcard},
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "kind group don't match",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Kinds: []Kinds{
					{
						Kinds:     []string{"notmatching", "neithermatching"},
						APIGroups: []string{Wildcard},
					},
					{
						Kinds:     []string{"notmatching", "kind"},
						APIGroups: []string{"notmatchinggroup"},
					},
				},
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "namespace matches",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "namespace", "name"),
			matcher: Match{
				Namespaces: []util.Wildcard{"nonmatching", "namespace"},
			},
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "namespace"}},
			wantMatch: true,
		},
		{
			name:   "is a matching Namespace",
			object: makeNamespace("matching"),
			matcher: Match{
				Namespaces: []util.Wildcard{"matching"},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "is not a matching Namespace",
			object: makeNamespace("non-matching"),
			matcher: Match{
				Namespaces: []util.Wildcard{"matching"},
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			// Ensures that namespaceMatch handles ns==nil
			name:   "namespaces configured, but cluster scoped",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Namespaces: []util.Wildcard{"nonmatching", "namespace"},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "namespace prefix matches",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "kube-system", "name"),
			matcher: Match{
				Namespaces: []util.Wildcard{"nonmatching", "kube-*"},
			},
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
			wantMatch: true,
		},
		{
			name:   "namespace is not in the matches list",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "namespace2", "name"),
			matcher: Match{
				Namespaces: []util.Wildcard{"nonmatching", "notmatchingeither"},
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "has namespace fails if cluster scoped",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "namespace", "name"),
			matcher: Match{
				Scope: apiextensionsv1.ClusterScoped,
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "has namespace succeeds if namespace scoped",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "namespace", "name"),
			matcher: Match{
				Scope: apiextensionsv1.NamespaceScoped,
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "has namespace succeeds if scope is typo",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "namespace", "name"),
			matcher: Match{
				Scope: "cluster",
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "without namespace succeeds if cluster scoped",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Scope: apiextensionsv1.ClusterScoped,
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "without namespace fails if namespace scoped",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				Scope: apiextensionsv1.NamespaceScoped,
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "is namespace succeeds if cluster scoped",
			object: makeNamespace("foo"),
			matcher: Match{
				Scope: apiextensionsv1.ClusterScoped,
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "is namespace fails if namespace scoped",
			object: makeNamespace("foo"),
			matcher: Match{
				Scope: apiextensionsv1.NamespaceScoped,
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "object's namespace is excluded",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "namespace", "name"),
			matcher: Match{
				ExcludedNamespaces: []util.Wildcard{"namespace"},
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "object is an excluded Namespace",
			object: makeNamespace("excluded"),
			matcher: Match{
				ExcludedNamespaces: []util.Wildcard{"excluded"},
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "object is not an excluded Namespace",
			object: makeNamespace("not-excluded"),
			matcher: Match{
				ExcludedNamespaces: []util.Wildcard{"excluded"},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			// Ensures that namespaceMatch handles ns==nil
			name:   "a namespace is excluded, but object is cluster scoped",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			matcher: Match{
				ExcludedNamespaces: []util.Wildcard{"namespace"},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "namespace is excluded by wildcard match",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "kube-system", "name"),
			matcher: Match{
				ExcludedNamespaces: []util.Wildcard{"kube-*"},
			},
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
			wantMatch: false,
		},
		{
			name: "label selector",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name", func(o *unstructured.Unstructured) {
				o.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			matcher: Match{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name: "invalid label selector",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name", func(o *unstructured.Unstructured) {
				o.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			matcher: Match{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Operator: "Invalid",
					}},
				},
			},
			namespace: nil,
			wantMatch: false,
			wantErr:   ErrMatch,
		},
		{
			name: "label selector not matching",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name", func(o *unstructured.Unstructured) {
				o.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			matcher: Match{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname":        "labelvalue",
						"labelnotmatching": "foo",
					},
				},
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "namespace selector",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
					Labels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			wantMatch: true,
		},
		{
			name:   "invalid namespace selector",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
					Labels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Operator: "Invalid",
					}},
				},
			},
			wantMatch: false,
			wantErr:   ErrMatch,
		},
		{
			name:   "namespace selector not matching",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "foo", "name"),
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
					Labels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
						"foo":       "bar",
					},
				},
			},
			wantMatch: false,
		},
		{
			name:      "namespace selector not matching, but cluster scoped",
			object:    makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name"),
			namespace: nil,
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
						"foo":       "bar",
					},
				},
			},
			wantMatch: true,
		},
		{
			name: "namespace selector is applied to the object, if the object is a namespace",
			object: makeNamespace("namespace", func(o *unstructured.Unstructured) {
				o.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			namespace: nil,
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "labelvalue",
					},
				},
			},
			wantMatch: true,
		},
		{
			name: "namespace selector is applied to the namespace, and does not match",
			object: makeNamespace("namespace", func(o *unstructured.Unstructured) {
				o.SetLabels(map[string]string{
					"labelname": "labelvalue",
				})
			}),
			namespace: nil,
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "badvalue",
					},
				},
			},
			wantMatch: false,
		},
		{
			name:      "namespace selector error on missing Namespace",
			object:    makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "foo", "name"),
			namespace: nil,
			matcher: Match{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"labelname": "badvalue",
					},
				},
			},
			wantMatch: false,
			wantErr:   ErrMatch,
		},
		{
			name:   "match name",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name-foo"),
			matcher: Match{
				Name: "name-foo",
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "match wildcard name",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name-foo"),
			matcher: Match{
				Name: "name-*",
			},
			namespace: nil,
			wantMatch: true,
		},
		{
			name:   "missing asterisk in name wildcard does not match",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name-foo"),
			matcher: Match{
				Name: "name-",
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "wrong name does not match",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "", "name-foo"),
			matcher: Match{
				Name: "name-bar",
			},
			namespace: nil,
			wantMatch: false,
		},
		{
			name:   "no match with correct name and wrong namespace",
			object: makeObject(schema.GroupVersionKind{Kind: "kind", Group: "group"}, "namespace", "name-foo"),
			matcher: Match{
				Name:       "name-foo",
				Namespaces: []util.Wildcard{"other-namespace"},
			},
			namespace: nil,
			wantMatch: false,
		},
	}

	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := Matches(&tc.matcher, tc.object, tc.namespace)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got Matches() err %v, want %v", err, tc.name)
			}
			if matches != tc.wantMatch {
				t.Errorf("%s: expecting match to be %v, was %v", tc.name, tc.wantMatch, matches)
			}
		})
	}
}

func makeObject(gvk schema.GroupVersionKind, namespace, name string, options ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: make(map[string]interface{})}
	obj.SetGroupVersionKind(gvk)
	obj.SetNamespace(namespace)
	obj.SetName(name)

	for _, o := range options {
		o(obj)
	}
	return obj
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
		name      string
		gvk       schema.GroupVersionKind
		applyTo   []ApplyTo
		wantApply bool
	}{
		{
			name: "exact match",
			gvk:  schema.GroupVersionKind{Group: "foo1", Version: "v1", Kind: "bar1"},
			applyTo: []ApplyTo{
				{
					Groups:   []string{"foo1"},
					Versions: []string{"v1"},
					Kinds:    []string{"bar1"},
				},
			},
			wantApply: true,
		},
		{
			name: "wrong group",
			gvk:  schema.GroupVersionKind{Group: "foo1", Version: "v1", Kind: "bar1"},
			applyTo: []ApplyTo{
				{
					Groups:   []string{"foo2"},
					Versions: []string{"v1"},
					Kinds:    []string{"bar1"},
				},
			},
			wantApply: false,
		},
		{
			name: "wrong version",
			gvk:  schema.GroupVersionKind{Group: "foo1", Version: "v1", Kind: "bar1"},
			applyTo: []ApplyTo{
				{
					Groups:   []string{"foo1"},
					Versions: []string{"v2"},
					Kinds:    []string{"bar1"},
				},
			},
			wantApply: false,
		},
		{
			name: "wrong Kind",
			gvk:  schema.GroupVersionKind{Group: "foo1", Version: "v1", Kind: "bar1"},
			applyTo: []ApplyTo{
				{
					Groups:   []string{"foo2"},
					Versions: []string{"v1"},
					Kinds:    []string{"bar1"},
				},
			},
			wantApply: false,
		},
		{
			name: "match one of each",
			gvk:  schema.GroupVersionKind{Group: "group", Version: "v1", Kind: "kind"},
			applyTo: []ApplyTo{
				{
					Groups:   []string{"aa", "bb", "group"},
					Versions: []string{"aa", "bb", "v1"},
					Kinds:    []string{"aa", "bb", "kind"},
				},
			},
			wantApply: true,
		},
		{
			name: "match second",
			gvk:  schema.GroupVersionKind{Group: "group", Version: "v1", Kind: "kind"},
			applyTo: []ApplyTo{
				{
					Groups:   []string{"group"},
					Versions: []string{"v1"},
					Kinds:    []string{"not matching"},
				},
				{
					Groups:   []string{"group"},
					Versions: []string{"v1"},
					Kinds:    []string{"kind"},
				},
			},
			wantApply: true,
		},
		{
			name: "match none",
			gvk:  schema.GroupVersionKind{Group: "foo1", Version: "v1", Kind: "bar1"},
			applyTo: []ApplyTo{
				{
					Groups:   []string{"foo2"},
					Versions: []string{"v1"},
					Kinds:    []string{"bar1"},
				},
				{
					Groups:   []string{"foo1"},
					Versions: []string{"v2"},
					Kinds:    []string{"bar1"},
				},
				{
					Groups:   []string{"foo1"},
					Versions: []string{"v1"},
					Kinds:    []string{"bar2"},
				},
			},
			wantApply: false,
		},
	}

	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			appliesTo := AppliesTo(tc.applyTo, tc.gvk)
			if appliesTo != tc.wantApply {
				t.Errorf("%s: expecting match to be %v, was %v", tc.name, tc.wantApply, appliesTo)
			}
		})
	}
}
