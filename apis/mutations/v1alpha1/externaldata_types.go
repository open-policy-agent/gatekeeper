package v1alpha1

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
)

// ExternalData describes the external data source to use for the mutation.
type ExternalData struct {
	// Provider is the name of the external data provider.
	// +kubebuilder:validation:Required
	Provider string `json:"provider,omitempty"`

	// DataSource specifies where to extract the data that will be sent
	// to the external data provider as parameters.
	// +kubebuilder:default="ValueAtLocation"
	DataSource types.ExternalDataSource `json:"dataSource,omitempty"`

	// FailurePolicy specifies the policy to apply when the external data
	// provider returns an error.
	// +kubebuilder:default="Fail"
	FailurePolicy types.ExternalDataFailurePolicy `json:"failurePolicy,omitempty"`

	// Default specifies the default value to use when the external data
	// provider returns an error and the failure policy is set to "UseDefault".
	Default string `json:"default,omitempty"`
}
