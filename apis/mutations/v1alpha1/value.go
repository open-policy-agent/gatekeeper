package v1alpha1

import (
	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
)

type AssignField struct {
	// Value is a constant value that will be assigned to `location`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:XPreserveUnknownFields
	Value *types.Anything `json:"value,omitempty"`

	// FromMetadata assigns a value from the specified metadata field.
	FromMetadata *FromMetadata `json:"fromMetadata,omitempty"`

	// ExternalData describes the external data provider to be used for mutation.
	ExternalData *ExternalData `json:"externalData,omitempty"`
}

type FromMetadata struct {
	// Field specifies which metadata field provides the assigned value. Valid fields are `namespace` and `name`.
	Field unversioned.Field `json:"field,omitempty"`
}
