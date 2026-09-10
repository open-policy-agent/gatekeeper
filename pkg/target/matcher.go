package target

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/constraints"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
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

	if ns == nil {
		ns = gkReq.getNamespace(m.cache)
	}

	return matchAny(m, ns, gkReq.source, obj, oldObj)
}

func matchAny(m *Matcher, ns *corev1.Namespace, source types.SourceType, objs ...*unstructured.Unstructured) (bool, error) {
	nilObj := 0
	for _, obj := range objs {
		if obj == nil || obj.Object == nil {
			nilObj++
			continue
		}

		t := &match.Matchable{
			Object:    obj,
			Namespace: ns,
			Source:    source,
		}
		matched, err := match.Matches(m.match, t)
		if err != nil {
			return false, fmt.Errorf("%w: %v :%w", ErrMatching, obj.GetName(), err)
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
	req.parsedObjectsOnce.Do(func() {
		req.parsedObjects.object, req.parsedObjects.err = unmarshalReviewObject("object", req.Object.Raw)
		if req.parsedObjects.err != nil {
			return
		}

		req.parsedObjects.oldObject, req.parsedObjects.err = unmarshalReviewObject("oldObject", req.OldObject.Raw)
	})

	return req.parsedObjects.object, req.parsedObjects.oldObject, req.namespace, req.parsedObjects.err
}

func unmarshalReviewObject(name string, raw []byte) (*unstructured.Unstructured, error) {
	if raw == nil {
		return nil, nil
	}

	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("%w: failed to unmarshal gkReview %s %s", ErrRequestObject, name, string(raw))
	}

	return obj, nil
}

func (g *gkReview) getNamespace(cache *nsCache) *corev1.Namespace {
	if g.namespace != nil || g.Namespace == "" {
		return g.namespace
	}

	g.cachedNamespaceOnce.Do(func() {
		if cache != nil {
			g.cachedNamespace = cache.GetNamespace(g.Namespace)
		}
	})
	return g.cachedNamespace
}
