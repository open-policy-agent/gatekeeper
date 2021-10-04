package fakes

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Pod creates a Pod for use in testing or debugging logic.
func Pod(opts ...Opt) *corev1.Pod {
	result := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Pod",
		},
	}

	opts = append(defaultNamespacedOpts, opts...)
	for _, opt := range opts {
		opt(result)
	}

	return result
}
