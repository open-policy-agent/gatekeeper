package fakes

import (
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SyncSetFor returns a syncset resource with the given name for the requested set of resources.
func SyncSetFor(name string, kinds []schema.GroupVersionKind) *syncsetv1alpha1.SyncSet {
	entries := make([]syncsetv1alpha1.GVKEntry, len(kinds))
	for i := range kinds {
		entries[i] = syncsetv1alpha1.GVKEntry(kinds[i])
	}

	return &syncsetv1alpha1.SyncSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: syncsetv1alpha1.GroupVersion.String(),
			Kind:       "SyncSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: syncsetv1alpha1.SyncSetSpec{
			GVKs: entries,
		},
	}
}

// ConfigFor returns a config resource with a SyncOnly containing the requested set of resources.
func ConfigFor(kinds []schema.GroupVersionKind) *configv1alpha1.Config {
	entries := make([]configv1alpha1.SyncOnlyEntry, len(kinds))
	for i := range kinds {
		entries[i] = configv1alpha1.SyncOnlyEntry(kinds[i])
	}

	return &configv1alpha1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configv1alpha1.GroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "config",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: entries,
			},
		},
	}
}
