// Package v1alpha1 contains API Schema definitions for the AuditTrigger v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=audit.gatekeeper.sh
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "audit.gatekeeper.sh", Version: "v1alpha1"}

	// SchemeBuilder is used to add Go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&AuditTrigger{},
		&AuditTriggerList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
