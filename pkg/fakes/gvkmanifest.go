package fakes

import (
	gvkmanifestv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/gvkmanifest/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GVKManifestFor returns a GVKManifest resource with the given name for the requested set of resources.
func GVKManifestFor(name string, gvks []schema.GroupVersionKind) *gvkmanifestv1alpha1.GVKManifest {
	groups := map[string]gvkmanifestv1alpha1.Versions{}
	for _, gvk := range gvks {
		if groups[gvk.Group] == nil {
			groups[gvk.Group] = gvkmanifestv1alpha1.Versions{}
		}
		if groups[gvk.Group][gvk.Version] == nil {
			groups[gvk.Group][gvk.Version] = gvkmanifestv1alpha1.Kinds{}
		}
		groups[gvk.Group][gvk.Version] = append(groups[gvk.Group][gvk.Version], gvk.Kind)
	}

	return &gvkmanifestv1alpha1.GVKManifest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvkmanifestv1alpha1.GroupVersion.String(),
			Kind:       "GVKManifest",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: gvkmanifestv1alpha1.GVKManifestSpec{
			Groups: groups,
		},
	}
}
