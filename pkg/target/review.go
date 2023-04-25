package target

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
)

type AugmentedReview struct {
	AdmissionRequest *admissionv1.AdmissionRequest
	Namespace        *corev1.Namespace
	Source           types.SourceType
}

type gkReview struct {
	admissionv1.AdmissionRequest
	namespace *corev1.Namespace
	source    types.SourceType
}

func (g *gkReview) GetAdmissionRequest() *admissionv1.AdmissionRequest {
	return &g.AdmissionRequest
}

