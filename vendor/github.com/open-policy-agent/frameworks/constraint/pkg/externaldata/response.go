package externaldata

// RegoResponse is the response inside rego.
type RegoResponse struct {
	Responses   [][]interface{} `json:"responses"`
	Errors      [][]interface{} `json:"errors"`
	StatusCode  int             `json:"status_code"`
	SystemError string          `json:"system_error"`
}

// ProviderResponse is the API response from a provider.
type ProviderResponse struct {
	APIVersion string   `json:"apiVersion,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Response   Response `json:"response,omitempty"`
}

// Response is the struct that holds the response from a provider.
type Response struct {
	Idempotent  bool   `json:"idempotent,omitempty"`
	Items       []Item `json:"items,omitempty"`
	SystemError string `json:"systemError,omitempty"`
}

// Items is the struct that contains the key, value or error from a provider response.
type Item struct {
	Key   interface{} `json:"key,omitempty"`
	Value interface{} `json:"value,omitempty"`
	Error string      `json:"error,omitempty"`
}

// NewRegoResponse creates a new rego response from the given provider response.
func NewRegoResponse(statusCode int, pr ProviderResponse) *RegoResponse {
	responses := make([][]interface{}, 0)
	errors := make([][]interface{}, 0)

	for _, item := range pr.Response.Items {
		if item.Error != "" {
			errors = append(errors, []interface{}{item.Key, item.Error})
		} else {
			responses = append(responses, []interface{}{item.Key, item.Value})
		}
	}

	return &RegoResponse{
		Responses:   responses,
		Errors:      errors,
		StatusCode:  statusCode,
		SystemError: pr.Response.SystemError,
	}
}
