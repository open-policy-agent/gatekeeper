package fakes

import (
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ConfigFor returns a config resource that watches the requested set of resources.
func ConfigFor(kinds []schema.GroupVersionKind) *configv1alpha1.Config {
	entries := make([]configv1alpha1.SyncOnlyEntry, len(kinds))
	for i := range kinds {
		entries[i].Group = kinds[i].Group
		entries[i].Version = kinds[i].Version
		entries[i].Kind = kinds[i].Kind
	}

	return &configv1alpha1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configv1alpha1.GroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "gatekeeper-system",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: entries,
			},
			Match: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"sync"},
				},
			},
		},
	}
}

// SyncSetFor returns a syncset resource with the given name for the requested set of resources.
func SyncSetFor(name string, kinds []schema.GroupVersionKind) *syncsetv1alpha1.SyncSet {
	entries := make([]syncsetv1alpha1.GVKEntry, len(kinds))
	for i := range kinds {
		entries[i] = syncsetv1alpha1.GVKEntry(kinds[i])
	}

	return &syncsetv1alpha1.SyncSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: syncsetv1alpha1.SyncSetSpec{
			GVKs: entries,
		},
	}
}

func UnstructuredFor(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	if namespace == "" {
		u.SetNamespace("default")
	} else {
		u.SetNamespace(namespace)
	}

	if gvk.Kind == "Pod" {
		u.Object["spec"] = map[string]interface{}{
			"containers": []map[string]interface{}{
				{
					"name":  "foo-container",
					"image": "foo-image",
				},
			},
		}
	}

	return u
}
