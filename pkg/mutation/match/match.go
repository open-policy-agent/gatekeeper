package match

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrMatch = errors.New("failed to run Match criteria")

// Wildcard represents matching any Group, Version, or Kind.
// Only for use in Match, not ApplyTo.
const Wildcard = "*"

// Match selects objects to apply mutations to.
// +kubebuilder:object:generate=true
type Match struct {
	Kinds []Kinds `json:"kinds,omitempty"`
	// Scope determines if cluster-scoped and/or namespaced-scoped resources
	// are matched.  Accepts `*`, `Cluster`, or `Namespaced`. (defaults to `*`)
	Scope apiextensionsv1.ResourceScope `json:"scope,omitempty"`
	// Namespaces is a list of namespace names. If defined, a constraint only
	// applies to resources in a listed namespace.  Namespaces also supports a
	// prefix or suffix based glob.  For example, `namespaces: [kube-*]` matches both
	// `kube-system` and `kube-public`, and `namespaces: [*-system]` matches both
	// `kube-system` and `gatekeeper-system`.
	Namespaces []util.Wildcard `json:"namespaces,omitempty"`
	// ExcludedNamespaces is a list of namespace names. If defined, a
	// constraint only applies to resources not in a listed namespace.
	// ExcludedNamespaces also supports a prefix or suffix based glob.  For example,
	// `excludedNamespaces: [kube-*]` matches both `kube-system` and
	// `kube-public`, and `excludedNamespaces: [*-system]` matches both `kube-system` and
	// `gatekeeper-system`.
	ExcludedNamespaces []util.Wildcard `json:"excludedNamespaces,omitempty"`
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
	Name util.Wildcard `json:"name,omitempty"`
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

// Matches verifies if the given object belonging to the given namespace
// matches Match. Only returns true if all parts of the Match succeed.
func Matches(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	if reflect.ValueOf(obj).IsNil() {
		// Simply checking if obj == nil is insufficient here.
		// obj can be an interface pointer to nil, such as client.Object(nil), which
		// is not equal to just "nil".
		return false, fmt.Errorf("%w: obj must be non-nil", ErrMatch)
	}

	// We fail the match if any of these returns false.
	topLevelMatchers := []matchFunc{
		kindsMatch,
		scopeMatch,
		namespacesMatch,
		excludedNamespacesMatch,
		labelSelectorMatch,
		namespaceSelectorMatch,
		namesMatch,
	}

	for _, fn := range topLevelMatchers {
		matches, err := fn(match, obj, ns)
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrMatch, err)
		}

		if !matches {
			// One of the matchers didn't match, so we can exit early.
			return false, nil
		}
	}

	return true, nil
}

// matchFunc defines the matching logic of a Top Level Matcher.  A TLM receives the match criteria,
// an object, and the namespace of the object and decides if there is a reason why the object does
// not match.  If the TLM associated with the matching function is not defined by the user, the
// matchFunc should return true.
type matchFunc func(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error)

func namespaceSelectorMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	if match.NamespaceSelector == nil {
		return true, nil
	}

	isNamespace := IsNamespace(obj)
	if !isNamespace && ns == nil && obj.GetNamespace() == "" {
		// Match all non-Namespace cluster-scoped objects.
		return true, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(match.NamespaceSelector)
	if err != nil {
		return false, err
	}

	if isNamespace {
		return selector.Matches(labels.Set(obj.GetLabels())), nil
	}

	if ns == nil {
		return false, fmt.Errorf("namespace selector for namespace-scoped object but missing Namespace")
	}

	return selector.Matches(labels.Set(ns.Labels)), nil
}

func labelSelectorMatch(match *Match, obj client.Object, _ *corev1.Namespace) (bool, error) {
	if match.LabelSelector == nil {
		return true, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(match.LabelSelector)
	if err != nil {
		return false, err
	}

	return selector.Matches(labels.Set(obj.GetLabels())), nil
}

func excludedNamespacesMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	// If we don't have a namespace, we can't disqualify the match
	var namespace string

	switch {
	case len(match.ExcludedNamespaces) == 0:
		return true, nil
	case IsNamespace(obj):
		namespace = obj.GetName()
	case ns != nil:
		namespace = ns.Name
	case obj.GetNamespace() != "":
		// Fall back to the Namespace the Object declares in case it isn't specified,
		// such as for the gator CLI.
		namespace = obj.GetNamespace()
	default:
		// obj is cluster-scoped and not a Namespace.
		return true, nil
	}

	for _, n := range match.ExcludedNamespaces {
		if n.Matches(namespace) {
			return false, nil
		}
	}

	return true, nil
}

func namespacesMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	// If we don't have a namespace, we can't disqualify the match
	var namespace string

	switch {
	case len(match.Namespaces) == 0:
		return true, nil
	case IsNamespace(obj):
		namespace = obj.GetName()
	case ns != nil:
		namespace = ns.Name
	case obj.GetNamespace() != "":
		// Fall back to the Namespace the Object declares in case it isn't specified,
		// such as for the gator CLI.
		namespace = obj.GetNamespace()
	default:
		return true, nil
	}

	for _, n := range match.Namespaces {
		if n.Matches(namespace) {
			return true, nil
		}
	}

	return false, nil
}

func kindsMatch(match *Match, obj client.Object, _ *corev1.Namespace) (bool, error) {
	if len(match.Kinds) == 0 {
		return true, nil
	}

	gvk := obj.GetObjectKind().GroupVersionKind()

	for _, kk := range match.Kinds {
		kindMatches := len(kk.Kinds) == 0 || contains(kk.Kinds, Wildcard) || contains(kk.Kinds, gvk.Kind)
		if !kindMatches {
			continue
		}

		groupMatches := len(kk.APIGroups) == 0 || contains(kk.APIGroups, Wildcard) || contains(kk.APIGroups, gvk.Group)
		if groupMatches {
			return true, nil
		}
	}

	return false, nil
}

func namesMatch(match *Match, obj client.Object, _ *corev1.Namespace) (bool, error) {
	// A blank string could be undefined or an intentional blank string by the user.  Either way,
	// we will assume this means "any name".  This goes with the undefined == match everything
	// pattern that we've already got going in the Match.
	if match.Name == "" {
		return true, nil
	}

	return match.Name.Matches(obj.GetName()), nil
}

func scopeMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	hasNamespace := obj.GetNamespace() != "" || ns != nil
	isNamespace := IsNamespace(obj)

	switch match.Scope {
	case apiextensionsv1.ClusterScoped:
		return isNamespace || !hasNamespace, nil
	case apiextensionsv1.NamespaceScoped:
		return !isNamespace && hasNamespace, nil
	default:
		// This includes invalid scopes, such as typos like "cluster" or "Namspace".
		return true, nil
	}
}

func IsNamespace(obj client.Object) bool {
	return obj.GetObjectKind().GroupVersionKind().Kind == "Namespace" &&
		obj.GetObjectKind().GroupVersionKind().Group == ""
}

// contains returns true is element is in set.
func contains(set []string, element string) bool {
	for _, s := range set {
		if s == element {
			return true
		}
	}
	return false
}
