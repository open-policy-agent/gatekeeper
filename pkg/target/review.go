package target

import (
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
)

type AugmentedReview struct {
	AdmissionRequest *admissionv1.AdmissionRequest
	Namespace        *corev1.Namespace
}

type gkReview struct {
	admissionv1.AdmissionRequest
	namespace *corev1.Namespace
}
