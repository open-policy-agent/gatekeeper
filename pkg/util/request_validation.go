package util

import (
	"errors"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint: revive // Moved error out of pkg/webhook/admission; needs capitalization for backwards compat.
var ErrOldObjectIsNil = errors.New("For admission webhooks registered for DELETE operations, please use Kubernetes v1.15.0+.")

// SetObjectOnDelete enforces that we use at least K8s API v1.15.0+ on DELETE operations
// and copies over the oldObject into the Object field for the given AdmissionRequest.
func SetObjectOnDelete(req *admission.Request) error {
	if req.AdmissionRequest.Operation == admissionv1.Delete {
		// oldObject is the existing object.
		// It is null for DELETE operations in API servers prior to v1.15.0.
		// https://github.com/kubernetes/website/pull/14671
		if req.AdmissionRequest.OldObject.Raw == nil {
			return ErrOldObjectIsNil
		}

		// For admission webhooks registered for DELETE operations on k8s built APIs or CRDs,
		// the apiserver now sends the existing object as admissionRequest.Request.OldObject to the webhook
		// object is the new object being admitted.
		// It is null for DELETE operations.
		// https://github.com/kubernetes/kubernetes/pull/76346
		req.AdmissionRequest.Object = req.AdmissionRequest.OldObject
	}

	return nil
}
