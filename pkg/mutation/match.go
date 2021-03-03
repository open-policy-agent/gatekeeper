package mutation

import (
	"errors"
	"fmt"

	mutationsv1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// Matches verifies if the given object belonging to the given namespace
// matches the current mutator.
func Matches(match mutationsv1.Match, obj runtime.Object, ns *corev1.Namespace) (bool, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return false, fmt.Errorf("accessor failed for %s", obj.GetObjectKind().GroupVersionKind().Kind)
	}

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

	if match.Scope == apiextensionsv1beta1.ClusterScoped &&
		!clusterScoped {
		return false, nil
	}

	if match.Scope == apiextensionsv1beta1.NamespaceScoped &&
		clusterScoped {
		return false, nil
	}

	if ns != nil {
		found := false
		for _, n := range match.Namespaces {
			if ns.Name == n {
				found = true
				break
			}
		}
		if !found && len(match.Namespaces) > 0 {
			return false, nil
		}

		for _, n := range match.ExcludedNamespaces {
			if ns.Name == n {
				return false, nil
			}
		}
		if match.LabelSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(match.LabelSelector)
			if err != nil {
				return false, err
			}
			if !selector.Matches(labels.Set(meta.GetLabels())) {
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
				if !selector.Matches(labels.Set(meta.GetLabels())) {
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

// AppliesTo checks if any item the given slice of ApplyTo applies to the given object
func AppliesTo(applyTo []mutationsv1.ApplyTo, obj runtime.Object) bool {
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
