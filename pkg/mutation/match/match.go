package match

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
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

// Matchable represent an object to be matched along with its metadata.
// +kubebuilder:object:generate=false
type Matchable struct {
	Object    client.Object
	Namespace *corev1.Namespace
	Source    types.SourceType
}

// Matches verifies if the given object belonging to the given namespace
// matches Match. Only returns true if all parts of the Match succeed.
func Matches(match *Match, target *Matchable) (bool, error) {
	if reflect.ValueOf(target.Object).IsNil() {
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
		sourceMatch,
	}

	for _, fn := range topLevelMatchers {
		matches, err := fn(match, target)
		if err != nil {
			return false, fmt.Errorf("%w: %w", ErrMatch, err)
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
type matchFunc func(match *Match, target *Matchable) (bool, error)

func namespaceSelectorMatch(match *Match, target *Matchable) (bool, error) {
	obj := target.Object
	ns := target.Namespace

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

func labelSelectorMatch(match *Match, target *Matchable) (bool, error) {
	obj := target.Object

	if match.LabelSelector == nil {
		return true, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(match.LabelSelector)
	if err != nil {
		return false, err
	}

	return selector.Matches(labels.Set(obj.GetLabels())), nil
}

func excludedNamespacesMatch(match *Match, target *Matchable) (bool, error) {
	obj := target.Object
	ns := target.Namespace

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

func namespacesMatch(match *Match, target *Matchable) (bool, error) {
	obj := target.Object
	ns := target.Namespace

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

func kindsMatch(match *Match, target *Matchable) (bool, error) {
	if len(match.Kinds) == 0 {
		return true, nil
	}

	gvk := target.Object.GetObjectKind().GroupVersionKind()

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

func namesMatch(match *Match, target *Matchable) (bool, error) {
	// A blank string could be undefined or an intentional blank string by the user.  Either way,
	// we will assume this means "any name".  This goes with the undefined == match everything
	// pattern that we've already got going in the Match.
	if match.Name == "" {
		return true, nil
	}

	return match.Name.Matches(target.Object.GetName()) || match.Name.MatchesGenerateName(target.Object.GetGenerateName()), nil
}

func scopeMatch(match *Match, target *Matchable) (bool, error) {
	hasNamespace := target.Object.GetNamespace() != "" || target.Namespace != nil
	isNamespace := IsNamespace(target.Object)

	switch match.Scope {
	case apiextensionsv1.ClusterScoped:
		return isNamespace || !hasNamespace, nil
	case apiextensionsv1.NamespaceScoped:
		return !isNamespace && hasNamespace, nil
	default:
		// This includes invalid scopes, such as typos like "cluster" or "Namespace".
		return true, nil
	}
}

func sourceMatch(match *Match, target *Matchable) (bool, error) {
	mSrc := types.SourceType(match.Source)
	tSrc := target.Source

	// An empty 'source' field will default to 'All'
	if mSrc == "" {
		mSrc = types.SourceTypeDefault
	} else if !types.IsValidSource(mSrc) {
		return false, fmt.Errorf("invalid source field %q", mSrc)
	}

	if tSrc == "" && mSrc != types.SourceTypeAll {
		return false, fmt.Errorf("source field not specified for resource %s", target.Object.GetName())
	}

	if mSrc == types.SourceTypeAll {
		return true, nil
	}

	if !types.IsValidSource(tSrc) {
		return false, fmt.Errorf("invalid source field %q", tSrc)
	}

	return mSrc == tSrc, nil
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
