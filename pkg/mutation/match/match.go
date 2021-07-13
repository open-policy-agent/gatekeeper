package match

import (
	"errors"
	"strings"

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
	Namespaces         []string                      `json:"namespaces,omitempty"`
	ExcludedNamespaces []string                      `json:"excludedNamespaces,omitempty"`
	LabelSelector      *metav1.LabelSelector         `json:"labelSelector,omitempty"`
	NamespaceSelector  *metav1.LabelSelector         `json:"namespaceSelector,omitempty"`
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

	foundMatch := false

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
			foundMatch = true
		}
	}
	if len(match.Kinds) == 0 {
		foundMatch = true
	}

	if !foundMatch {
		return false, nil
	}

	clusterScoped := ns == nil || isNamespace(obj)

	if match.Scope == apiextensionsv1.ClusterScoped &&
		!clusterScoped {
		return false, nil
	}

	if match.Scope == apiextensionsv1.NamespaceScoped &&
		clusterScoped {
		return false, nil
	}

	if ns != nil {
		found := false
		for _, n := range match.Namespaces {
			if ns.Name == n || prefixMatch(n, ns.Name) {
				found = true
				break
			}
		}
		if !found && len(match.Namespaces) > 0 {
			return false, nil
		}

		for _, n := range match.ExcludedNamespaces {
			if ns.Name == n || prefixMatch(n, ns.Name) {
				return false, nil
			}
		}
		if match.LabelSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(match.LabelSelector)
			if err != nil {
				return false, err
			}
			if !selector.Matches(labels.Set(obj.GetLabels())) {
				return false, nil
			}
		}

		if match.NamespaceSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(match.NamespaceSelector)
			if err != nil {
				return false, err
			}

			switch {
			case isNamespace(obj): // if the object is a namespace, namespace selector matches against the object
				if !selector.Matches(labels.Set(obj.GetLabels())) {
					return false, nil
				}
			case clusterScoped:
				// cluster scoped, matches by default
			case !selector.Matches(labels.Set(ns.Labels)):
				return false, nil
			}
		}
	}

	return true, nil
}

// prefixMatch matches checks if the candidate contains the prefix defined in the source.
// The source is expected to end with a "*", which acts as a glob.  It is removed when
// performing the prefix-based match.
func prefixMatch(source, candidate string) bool {
	if !strings.HasSuffix(source, "*") {
		return false
	}

	return strings.HasPrefix(candidate, strings.TrimSuffix(source, "*"))
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
