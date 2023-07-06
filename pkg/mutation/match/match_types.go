// +kubebuilder:object:generate=true
// +groupName=match.gatekeeper.sh
package match

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Match selects which objects are in scope.
// +kubebuilder:object:generate=true
type Match struct {
	// Source determines whether generated or original resources are matched.
	// Accepts `Generated`|`Original`|`All` (defaults to `All`). A value of
	// `Generated` will only match generated resources, while `Original` will only
	// match regular resources.
	// +kubebuilder:validation:Enum=All;Generated;Original
	Source string  `json:"source,omitempty"`
	Kinds  []Kinds `json:"kinds,omitempty"`
	// Scope determines if cluster-scoped and/or namespaced-scoped resources
	// are matched.  Accepts `*`, `Cluster`, or `Namespaced`. (defaults to `*`)
	Scope apiextensionsv1.ResourceScope `json:"scope,omitempty"`
	// Namespaces is a list of namespace names. If defined, a constraint only
	// applies to resources in a listed namespace.  Namespaces also supports a
	// prefix or suffix based glob.  For example, `namespaces: [kube-*]` matches both
	// `kube-system` and `kube-public`, and `namespaces: [*-system]` matches both
	// `kube-system` and `gatekeeper-system`.
	Namespaces []wildcard.Wildcard `json:"namespaces,omitempty"`
	// ExcludedNamespaces is a list of namespace names. If defined, a
	// constraint only applies to resources not in a listed namespace.
	// ExcludedNamespaces also supports a prefix or suffix based glob.  For example,
	// `excludedNamespaces: [kube-*]` matches both `kube-system` and
	// `kube-public`, and `excludedNamespaces: [*-system]` matches both `kube-system` and
	// `gatekeeper-system`.
	ExcludedNamespaces []wildcard.Wildcard `json:"excludedNamespaces,omitempty"`
	// LabelSelector is the combination of two optional fields: `matchLabels`
	// and `matchExpressions`.  These two fields provide different methods of
	// selecting or excluding k8s objects based on the label keys and values
	// included in object metadata.  All selection expressions from both
	// sections are ANDed to determine if an object meets the cumulative
	// requirements of the selector.
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
	// NamespaceSelector is a label selector against an object's containing
	// namespace or the object itself, if the object is a namespace.
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	// Name is the name of an object.  If defined, it will match against objects with the specified
	// name.  Name also supports a prefix or suffix glob.  For example, `name: pod-*` would match
	// both `pod-a` and `pod-b`, and `name: *-pod` would match both `a-pod` and `b-pod`.
	Name wildcard.Wildcard `json:"name,omitempty"`
}

// Kinds accepts a list of objects with apiGroups and kinds fields
// that list the groups/kinds of objects to which the mutation will apply.
// If multiple groups/kinds objects are specified,
// only one match is needed for the resource to be in scope.
// +kubebuilder:object:generate=true
type Kinds struct {
	// APIGroups is the API groups the resources belong to. '*' is all groups.
	// If '*' is present, the length of the slice must be one.
	// Required.
	APIGroups []string `json:"apiGroups,omitempty" protobuf:"bytes,1,rep,name=apiGroups"`
	Kinds     []string `json:"kinds,omitempty"`
}

// DummyCRD is a "dummy" CRD to hold the Match object, which we ultimately
// need to generate JSONSchemaProps. The TypeMeta and ObjectMeta fields are
// required for controller-gen to generate the CRD.
// +kubebuilder:resource:path="matchcrd"
type DummyCRD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadataDummy,omitempty"`

	Match `json:"embeddedMatch,omitempty"`
}
