package target

import (
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type AugmentedReview struct {
	AdmissionRequest *admissionv1.AdmissionRequest
	Namespace        *corev1.Namespace
	Source           types.SourceType
	IsAdmission      bool
}

type gkReview struct {
	admissionv1.AdmissionRequest
	namespace   *corev1.Namespace
	source      types.SourceType
	isAdmission bool

	// parsedObjects is request-local state shared by per-constraint Match calls.
	// It avoids repeatedly unmarshalling the same AdmissionReview payload while
	// keeping the raw AdmissionRequest immutable from the matcher's perspective.
	parsedObjectsOnce sync.Once
	parsedObjects     parsedReviewObjects

	// cachedNamespace stores the namespace cache lookup for this request.
	// namespace remains the explicit namespace supplied by AugmentedReview.
	cachedNamespaceOnce sync.Once
	cachedNamespace     *corev1.Namespace
}

type parsedReviewObjects struct {
	object    *unstructured.Unstructured
	oldObject *unstructured.Unstructured
	err       error
}

func (g *gkReview) GetAdmissionRequest() *admissionv1.AdmissionRequest {
	return &g.AdmissionRequest
}

func (g *gkReview) IsAdmissionRequest() bool {
	return g.isAdmission
}
