package fakes

import (
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
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: syncsetv1alpha1.SyncSetSpec{
			GVKs: entries,
		},
	}
}
