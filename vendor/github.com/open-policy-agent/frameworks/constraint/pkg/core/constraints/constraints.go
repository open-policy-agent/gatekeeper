package constraints

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SemanticEqualWithLabelsAndAnnotations returns whether the specs of the constraints are equal. It
// ignores status and most metadata because neither are relevant as to how a
// constraint is enforced. It is assumed that the author is comparing
// two constraints with the same GVK/namespace/name. Labels are compared
// because the labels of a constraint may impact functionality (e.g. whether
// a constraint is expected to be enforced by Kubernetes' Validating Admission Policy).
// Annotations are also compared as they may be used for audit logging and export functionality, even though they do not impact enforcement.
func SemanticEqualWithLabelsAndAnnotations(c1 *unstructured.Unstructured, c2 *unstructured.Unstructured) bool {
	if c1 == nil || c2 == nil {
		return c1 == c2
	}

	if c1.Object == nil || c2.Object == nil {
		return (c1.Object == nil) == (c2.Object == nil)
	}

	s1 := c1.Object["spec"]
	s2 := c2.Object["spec"]

	l1, _, _ := unstructured.NestedFieldNoCopy(c1.Object, "metadata", "labels")
	l2, _, _ := unstructured.NestedFieldNoCopy(c2.Object, "metadata", "labels")

	a1, _, _ := unstructured.NestedFieldNoCopy(c1.Object, "metadata", "annotations")
	a2, _, _ := unstructured.NestedFieldNoCopy(c2.Object, "metadata", "annotations")

	return reflect.DeepEqual(s1, s2) && reflect.DeepEqual(l1, l2) && reflect.DeepEqual(a1, a2)
}

// Matcher matches object review requests.
type Matcher interface {
	// Match returns true if the Matcher's Constraint should run against the
	// passed review object.
	// Note that this is the review object returned by HandleReview.
	Match(review interface{}) (bool, error)
}
