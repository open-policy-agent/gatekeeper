package routing

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsManagementResource(t *testing.T) {
	tests := []struct {
		name string
		gvk  schema.GroupVersionKind
		want bool
	}{
		// PodStatus types — should route to management
		{
			name: "ConstraintTemplatePodStatus",
			gvk:  schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplatePodStatus"},
			want: true,
		},
		{
			name: "ConstraintPodStatus",
			gvk:  schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintPodStatus"},
			want: true,
		},
		{
			name: "ConfigPodStatus",
			gvk:  schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConfigPodStatus"},
			want: true,
		},
		{
			name: "MutatorPodStatus",
			gvk:  schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "MutatorPodStatus"},
			want: true,
		},
		{
			name: "ProviderPodStatus",
			gvk:  schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ProviderPodStatus"},
			want: true,
		},
		{
			name: "ExpansionTemplatePodStatus",
			gvk:  schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ExpansionTemplatePodStatus"},
			want: true,
		},
		{
			name: "ConnectionPodStatus v1alpha1",
			gvk:  schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1alpha1", Kind: "ConnectionPodStatus"},
			want: true,
		},
		// Policy/parent resources — should route to target
		{
			name: "ConstraintTemplate",
			gvk:  schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"},
			want: false,
		},
		{
			name: "Config",
			gvk:  schema.GroupVersionKind{Group: "config.gatekeeper.sh", Version: "v1alpha1", Kind: "Config"},
			want: false,
		},
		{
			name: "Provider",
			gvk:  schema.GroupVersionKind{Group: "externaldata.gatekeeper.sh", Version: "v1beta1", Kind: "Provider"},
			want: false,
		},
		{
			name: "Connection",
			gvk:  schema.GroupVersionKind{Group: "connection.gatekeeper.sh", Version: "v1alpha1", Kind: "Connection"},
			want: false,
		},
		{
			name: "Assign",
			gvk:  schema.GroupVersionKind{Group: "mutations.gatekeeper.sh", Version: "v1", Kind: "Assign"},
			want: false,
		},
		{
			name: "ExpansionTemplate",
			gvk:  schema.GroupVersionKind{Group: "expansion.gatekeeper.sh", Version: "v1beta1", Kind: "ExpansionTemplate"},
			want: false,
		},
		{
			name: "SyncSet",
			gvk:  schema.GroupVersionKind{Group: "syncset.gatekeeper.sh", Version: "v1alpha1", Kind: "SyncSet"},
			want: false,
		},
		{
			name: "Constraint (dynamic kind)",
			gvk:  schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"},
			want: false,
		},
		{
			name: "CRD",
			gvk:  schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
			want: false,
		},
		{
			name: "Namespace",
			gvk:  schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"},
			want: false,
		},
		{
			name: "empty GVK",
			gvk:  schema.GroupVersionKind{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsManagementResource(tt.gvk)
			if got != tt.want {
				t.Errorf("IsManagementResource(%v) = %v, want %v", tt.gvk, got, tt.want)
			}
		})
	}
}
