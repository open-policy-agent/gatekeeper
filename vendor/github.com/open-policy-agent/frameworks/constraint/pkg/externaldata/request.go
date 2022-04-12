package externaldata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
)

// RegoRequest is the request for external_data rego function.
type RegoRequest struct {
	// ProviderName is the name of the external data provider.
	ProviderName string `json:"provider"`
	// Keys is the list of keys to send to the external data provider.
	Keys []string `json:"keys"`
}

// ProviderRequest is the API request for the external data provider.
type ProviderRequest struct {
	// APIVersion is the API version of the external data provider.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is kind of the external data provider API call. This can be "ProviderRequest" or "ProviderResponse".
	Kind ProviderKind `json:"kind,omitempty"`
	// Request contains the request for the external data provider.
	Request Request `json:"request,omitempty"`
}

// Request is the struct that contains the keys to query.
type Request struct {
	// Keys is the list of keys to send to the external data provider.
	Keys []string `json:"keys,omitempty"`
}

// NewProviderRequest creates a new request for the external data provider.
func NewProviderRequest(keys []string) *ProviderRequest {
	return &ProviderRequest{
		APIVersion: "externaldata.gatekeeper.sh/v1alpha1",
		Kind:       "ProviderRequest",
		Request: Request{
			Keys: keys,
		},
	}
}

// SendRequestToProvider is a function that sends a request to the external data provider.
type SendRequestToProvider func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*ProviderResponse, int, error)

// DefaultSendRequestToProvider is the default function to send the request to the external data provider.
func DefaultSendRequestToProvider(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*ProviderResponse, int, error) {
	externaldataRequest := NewProviderRequest(keys)
	body, err := json.Marshal(externaldataRequest)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to marshal external data request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, provider.Spec.URL, bytes.NewBuffer(body))
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to create external data request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	ctxWithDeadline, cancel := context.WithDeadline(ctx, time.Now().Add(time.Duration(provider.Spec.Timeout)*time.Second))
	defer cancel()

	resp, err := http.DefaultClient.Do(req.WithContext(ctxWithDeadline))
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to send external data request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read external data response: %w", err)
	}

	var externaldataResponse ProviderResponse
	if err := json.Unmarshal(respBody, &externaldataResponse); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to unmarshal external data response: %w", err)
	}

	return &externaldataResponse, resp.StatusCode, nil
}

// ProviderKind strings are special string constants for Providers.
// +kubebuilder:validation:Enum=ProviderRequestKind;ProviderResponseKind
type ProviderKind string

const (
	// ProviderRequestKind is the kind of the request.
	ProviderRequestKind ProviderKind = "ProviderRequest"
	// ProviderResponseKind is the kind of the response.
	ProviderResponseKind ProviderKind = "ProviderResponse"
)
