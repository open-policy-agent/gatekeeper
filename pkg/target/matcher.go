package target

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ constraints.Matcher = &Matcher{}

// Matcher implements constraint.Matcher.
type Matcher struct {
	match *match.Match
	cache *nsCache
}

func (m *Matcher) Match(review interface{}) (bool, error) {
	if m.match == nil {
		// No-op if Match unspecified.
		return true, nil
	}

	gkReq, ok := review.(*gkReview)
	if !ok {
		return false, fmt.Errorf("%w: expect %T, got %T", ErrReviewFormat, &gkReview{}, review)
	}

	obj, oldObj, ns, err := gkReviewToObject(gkReq)
	if err != nil {
		return false, err
	}

	if (ns == nil) && (gkReq.Namespace != "") {
		ns = m.cache.GetNamespace(gkReq.Namespace)
	}

	return matchAny(m, ns, obj, oldObj)
}

func matchAny(m *Matcher, ns *corev1.Namespace, objs ...*unstructured.Unstructured) (bool, error) {
	nilObj := 0
	for _, obj := range objs {
		if obj == nil || obj.Object == nil {
			nilObj++
			continue
		}

		matched, err := match.Matches(m.match, obj, ns)
		if err != nil {
			return false, fmt.Errorf("%w: %v", ErrMatching, err)
		}

		if matched {
			return true, nil
		}
	}

	if nilObj == len(objs) {
		return false, fmt.Errorf("%w: neither object nor old object are defined", ErrRequestObject)
	}
	return false, nil
}

func gkReviewToObject(req *gkReview) (*unstructured.Unstructured, *unstructured.Unstructured, *corev1.Namespace, error) {
	var obj *unstructured.Unstructured
	if req.Object.Raw != nil {
		obj = &unstructured.Unstructured{}
		err := obj.UnmarshalJSON(req.Object.Raw)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: failed to unmarshal gkReview object %s", ErrRequestObject, string(req.Object.Raw))
		}
	}

	var oldObj *unstructured.Unstructured
	if req.OldObject.Raw != nil {
		oldObj = &unstructured.Unstructured{}
		err := oldObj.UnmarshalJSON(req.OldObject.Raw)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: failed to unmarshal gkReview oldObject %s", ErrRequestObject, string(req.OldObject.Raw))
		}
	}

	return obj, oldObj, req.namespace, nil
}
