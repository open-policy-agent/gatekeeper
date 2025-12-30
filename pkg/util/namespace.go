package util

import (
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	corev1 "k8s.io/api/core/v1"
)

// NamespaceReviewOpt converts a corev1.Namespace to a reviews.ReviewOpt for passing
// namespace context to the constraint client. Returns nil if the namespace is nil
// or if conversion fails (error is logged).
func NamespaceReviewOpt(ns *corev1.Namespace, log logr.Logger) reviews.ReviewOpt {
	if ns == nil {
		return nil
	}
	nsMap, err := NamespaceToMap(ns)
	if err != nil {
		log.Error(err, "failed to convert namespace to map, continuing without namespace context")
		return nil
	}
	return reviews.Namespace(nsMap)
}

// NamespaceToMap converts a corev1.Namespace to map[string]interface{} for passing
// to the constraint client. This enables CEL expressions to use namespaceObject
// and Rego policies to access input.review.namespaceObject.
func NamespaceToMap(ns *corev1.Namespace) (map[string]interface{}, error) {
	if ns == nil {
		return nil, nil
	}

	data, err := json.Marshal(ns)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal namespace: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal namespace: %w", err)
	}

	return result, nil
}
