package externaldata

// RegoRequest is the request for external_data rego function.
type RegoRequest struct {
	ProviderName string        `json:"provider"`
	Keys         []interface{} `json:"keys"`
}

// ProviderRequest is the API request for the external data provider.
type ProviderRequest struct {
	APIVersion string  `json:"apiVersion,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	Request    Request `json:"request,omitempty"`
}

// Request is the struct that contains the keys to query.
type Request struct {
	Keys []interface{} `json:"keys,omitempty"`
}

// NewRequest creates a new request for the external data provider.
func NewProviderRequest(keys []interface{}) *ProviderRequest {
	return &ProviderRequest{
		APIVersion: "externaldata.gatekeeper.sh/v1",
		Kind:       "ProviderRequest",
		Request: Request{
			Keys: keys,
		},
	}
}
