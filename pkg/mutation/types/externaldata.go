package types

import (
	"context"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
)

// SendRequestToExternalDataProvider is a function that sends a request to the external data provider.
type SendRequestToExternalDataProvider func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*frameworksexternaldata.ProviderResponse, error)

// ExternalDataSource is the type of the data source to use for the external data.
// +kubebuilder:validation:Enum=ValueAtLocation;Username
type ExternalDataSource string

const (
	// ValueAtLocation indicates that the value at spec.location of the mutation
	// spec will be extracted to the external data provider as the data source.
	DataSourceValueAtLocation ExternalDataSource = "ValueAtLocation"

	// Username indicates that the username of the admission request will
	// be extracted to the external data provider as the data source.
	DataSourceUsername ExternalDataSource = "Username"
)

// ExternalDataFailurePolicy is the type of the failure policy to use for the external data.
// +kubebuilder:validation:Enum=UseDefault;Ignore;Fail
type ExternalDataFailurePolicy string

const (
	// UseDefault indicates that the default value of the external
	// data provider will be used.
	FailurePolicyUseDefault ExternalDataFailurePolicy = "UseDefault"

	// Ignore indicates that the mutation will be ignored if the external
	// data provider fails.
	FailurePolicyIgnore ExternalDataFailurePolicy = "Ignore"

	// Fail indicates that the mutation will be failed if the external
	// data provider fails.
	FailurePolicyFail ExternalDataFailurePolicy = "Fail"
)
