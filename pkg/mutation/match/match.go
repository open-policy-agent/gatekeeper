package match

import (
	"errors"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyTo determines what GVKs items the mutation should apply to.
// Globs are not allowed.
// +kubebuilder:object:generate=true
type ApplyTo struct {
	Groups   []string `json:"groups,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
	Versions []string `json:"versions,omitempty"`
}

// Flatten returns the set of GroupVersionKinds this ApplyTo matches.
// The GVKs are not guaranteed to be sorted or unique.
func (a ApplyTo) Flatten() []schema.GroupVersionKind {
	var result []schema.GroupVersionKind
	for _, group := range a.Groups {
		for _, version := range a.Versions {
			for _, kind := range a.Kinds {
				gvk := schema.GroupVersionKind{
					Group:   group,
					Version: version,
					Kind:    kind,
				}
				result = append(result, gvk)
			}
		}
	}
	return result
}

// Match selects objects to apply mutations to.
// +kubebuilder:object:generate=true
type Match struct {
	Kinds              []Kinds                       `json:"kinds,omitempty"`
	Scope              apiextensionsv1.ResourceScope `json:"scope,omitempty"`
	Namespaces         []util.PrefixWildcard         `json:"namespaces,omitempty"`
	ExcludedNamespaces []util.PrefixWildcard         `json:"excludedNamespaces,omitempty"`
	LabelSelector      *metav1.LabelSelector         `json:"labelSelector,omitempty"`
	NamespaceSelector  *metav1.LabelSelector         `json:"namespaceSelector,omitempty"`
	// Name is the name of an object.  If defined, it will match against objects with the specified
	// name.  Name also supports a prefix-based glob.  For example, `name: pod-*` would match both
	// `pod-a` and `pod-b`.
	Name util.PrefixWildcard `json:"name,omitempty"`
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
// matches the current mutator.
func Matches(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	if isNamespace(obj) && ns == nil {
		return false, errors.New("invalid call to Matches(), ns must not be nil for Namespace objects")
	}

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
		ok, err := fn(match, obj, ns)
		if err != nil {
			return false, err
		}
		if !ok {
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

	clusterScoped := ns == nil || isNamespace(obj)

	selector, err := metav1.LabelSelectorAsSelector(match.NamespaceSelector)
	if err != nil {
		return false, err
	}

	switch {
	case isNamespace(obj): // if the object is a namespace, namespace selector matches against the object
		return selector.Matches(labels.Set(obj.GetLabels())), nil
	case clusterScoped:
		return true, nil
	}

	return selector.Matches(labels.Set(ns.Labels)), nil
}

func labelSelectorMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
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
	if ns == nil {
		return true, nil
	}

	for _, n := range match.ExcludedNamespaces {
		if n.Matches(ns.Name) {
			return false, nil
		}
	}

	return true, nil
}

func namespacesMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	// If we don't have a namespace, we can't disqualify the match
	if ns == nil {
		return true, nil
	}

	for _, n := range match.Namespaces {
		if n.Matches(ns.Name) {
			return true, nil
		}
	}

	if len(match.Namespaces) > 0 {
		return false, nil
	}

	return true, nil
}

func kindsMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	if len(match.Kinds) == 0 {
		return true, nil
	}

	for _, kk := range match.Kinds {
		kindMatches := false
		groupMatches := false

		for _, k := range kk.Kinds {
			if k == "*" || k == obj.GetObjectKind().GroupVersionKind().Kind {
				kindMatches = true
				break
			}
		}
		if len(kk.Kinds) == 0 {
			kindMatches = true
		}

		for _, g := range kk.APIGroups {
			if g == "*" || g == obj.GetObjectKind().GroupVersionKind().Group {
				groupMatches = true
				break
			}
		}
		if len(kk.APIGroups) == 0 {
			groupMatches = true
		}

		if kindMatches && groupMatches {
			return true, nil
		}
	}

	return false, nil
}

func namesMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	// A blank string could be undefined or an intentional blank string by the user.  Either way,
	// we will assume this means "any name".  This goes with the undefined == match everything
	// pattern that we've already got going in the Match.
	if match.Name == "" {
		return true, nil
	}

	return match.Name.Matches(obj.GetName()), nil
}

func scopeMatch(match *Match, obj client.Object, ns *corev1.Namespace) (bool, error) {
	clusterScoped := ns == nil || isNamespace(obj)

	if match.Scope == apiextensionsv1.ClusterScoped &&
		!clusterScoped {
		return false, nil
	}

	if match.Scope == apiextensionsv1.NamespaceScoped &&
		clusterScoped {
		return false, nil
	}

	return true, nil
}

// AppliesTo checks if any item the given slice of ApplyTo applies to the given object.
func AppliesTo(applyTo []ApplyTo, obj runtime.Object) bool {
	gvk := obj.GetObjectKind().GroupVersionKind()
	for _, apply := range applyTo {
		matchesGroup := false
		matchesVersion := false
		matchesKind := false

		for _, g := range apply.Groups {
			if g == gvk.Group {
				matchesGroup = true
				break
			}
		}
		for _, g := range apply.Versions {
			if g == gvk.Version {
				matchesVersion = true
				break
			}
		}
		for _, g := range apply.Kinds {
			if g == gvk.Kind {
				matchesKind = true
				break
			}
		}
		if matchesGroup &&
			matchesVersion &&
			matchesKind {
			return true
		}
	}
	return false
}

func isNamespace(obj runtime.Object) bool {
	return obj.GetObjectKind().GroupVersionKind().Kind == "Namespace" &&
		obj.GetObjectKind().GroupVersionKind().Group == ""
}
