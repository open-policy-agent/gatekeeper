package core

import (
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	apiTypes "k8s.io/apimachinery/pkg/types"
)

type statusUpdate func(status *statusv1beta1.MutatorPodStatus)

func setID(id apiTypes.UID) statusUpdate {
	return func(status *statusv1beta1.MutatorPodStatus) {
		status.Status.MutatorUID = id
	}
}

func setGeneration(generation int64) statusUpdate {
	return func(status *statusv1beta1.MutatorPodStatus) {
		status.Status.ObservedGeneration = generation
	}
}

func setErrors(err error) statusUpdate {
	return func(status *statusv1beta1.MutatorPodStatus) {
		// Replaces any existing errors, if there was one.
		switch err.(type) {
		case nil:
			status.Status.Errors = nil
		case schema.ErrConflictingSchema:
			status.Status.Errors = []statusv1beta1.MutatorError{{
				Type:    schema.ErrConflictingSchemaType,
				Message: err.Error(),
			}}
		default:
			status.Status.Errors = []statusv1beta1.MutatorError{{Message: err.Error()}}
		}
	}
}

func replaceErrorIfConflictingSchema(err error) statusUpdate {
	return func(status *statusv1beta1.MutatorPodStatus) {
		for _, err := range status.Status.Errors {
			// Don't replace non-ErrConflictingSchema errors.
			if err.Type != schema.ErrConflictingSchemaType {
				return
			}
		}

		setErrors(err)(status)
	}
}

func setEnforced(isEnforced bool) statusUpdate {
	return func(status *statusv1beta1.MutatorPodStatus) {
		status.Status.Enforced = isEnforced
	}
}
