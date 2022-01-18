package constraints

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SemanticEqual returns whether the specs of the constraints are equal. It
// ignores status and metadata because neither are relevant as to how a
// constraint is enforced. It is assumed that the author is comparing
// two constraints with the same GVK/namespace/name.
func SemanticEqual(c1 *unstructured.Unstructured, c2 *unstructured.Unstructured) bool {
	if c1 == nil || c2 == nil {
		return c1 == c2
	}

	if c1.Object == nil || c2.Object == nil {
		return (c1.Object == nil) == (c2.Object == nil)
	}

	s1 := c1.Object["spec"]
	s2 := c2.Object["spec"]
	return reflect.DeepEqual(s1, s2)
}

// Matcher matches object review requests.
type Matcher interface {
	// Match returns true if the Matcher's Constraint should run against the
	// passed review object.
	// Note that this is the review object returned by HandleReview.
	Match(review interface{}) (bool, error)
}
