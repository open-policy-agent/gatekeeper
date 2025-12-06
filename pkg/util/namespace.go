package util

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

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
