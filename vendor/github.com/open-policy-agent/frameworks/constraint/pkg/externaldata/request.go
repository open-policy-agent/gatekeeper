package externaldata

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
)

const (
	HTTPScheme  = "http"
	HTTPSScheme = "https"
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
		APIVersion: "externaldata.gatekeeper.sh/v1beta1",
		Kind:       "ProviderRequest",
		Request: Request{
			Keys: keys,
		},
	}
}

// SendRequestToProvider is a function that sends a request to the external data provider.
type SendRequestToProvider func(ctx context.Context, provider *unversioned.Provider, keys []string, clientCert *tls.Certificate) (*ProviderResponse, int, error)

// DefaultSendRequestToProvider is the default function to send the request to the external data provider.
func DefaultSendRequestToProvider(ctx context.Context, provider *unversioned.Provider, keys []string, clientCert *tls.Certificate) (*ProviderResponse, int, error) {
	externaldataRequest := NewProviderRequest(keys)
	body, err := json.Marshal(externaldataRequest)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to marshal external data request: %w", err)
	}

	client, err := getClient(provider, clientCert)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to get HTTP client: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, provider.Spec.URL, bytes.NewBuffer(body))
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to create external data request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	ctxWithDeadline, cancel := context.WithDeadline(ctx, time.Now().Add(time.Duration(provider.Spec.Timeout)*time.Second))
	defer cancel()

	resp, err := client.Do(req.WithContext(ctxWithDeadline))
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to send external data request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read external data response: %w", err)
	}

	var externaldataResponse ProviderResponse
	if err := json.Unmarshal(respBody, &externaldataResponse); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to unmarshal external data response: %w", err)
	}

	return &externaldataResponse, resp.StatusCode, nil
}

// getClient returns a new HTTP client, and set up its TLS configuration.
func getClient(provider *unversioned.Provider, clientCert *tls.Certificate) (*http.Client, error) {
	u, err := url.Parse(provider.Spec.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider URL %s: %w", provider.Spec.URL, err)
	}

	if u.Scheme != HTTPSScheme {
		return nil, fmt.Errorf("only HTTPS scheme is supported")
	}

	client := &http.Client{
		Timeout: time.Duration(provider.Spec.Timeout) * time.Second,
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}

	// present our client cert to the server
	// in case provider wants to verify it
	if clientCert != nil {
		tlsConfig.Certificates = []tls.Certificate{*clientCert}
	}

	// if the provider presents its own CA bundle,
	// we will use it to verify the server's certificate
	caBundleData, err := base64.StdEncoding.DecodeString(provider.Spec.CABundle)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA bundle: %w", err)
	}

	providerCertPool := x509.NewCertPool()
	if ok := providerCertPool.AppendCertsFromPEM(caBundleData); !ok {
		return nil, fmt.Errorf("failed to append provider's CA bundle to certificate pool")
	}

	tlsConfig.RootCAs = providerCertPool

	client.Transport = &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return client, nil
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
