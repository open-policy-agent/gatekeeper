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
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
)

const (
	// HTTPScheme represents the HTTP URL scheme.
	HTTPScheme = "http"
	// HTTPSScheme represents the HTTPS URL scheme.
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

// defaultClientCache is a package-level cache used by DefaultSendRequestToProvider
// to reuse HTTP clients per provider, preventing goroutine leaks from orphaned transports.
var defaultClientCache = NewClientCache()

// DefaultClientCache returns the package-level ClientCache used by
// DefaultSendRequestToProvider. Use this to wire invalidation into
// a ProviderCache via SetClientCache.
func DefaultClientCache() *ClientCache {
	return defaultClientCache
}

// DefaultSendRequestToProvider is the default function to send the request to the external data provider.
func DefaultSendRequestToProvider(ctx context.Context, provider *unversioned.Provider, keys []string, clientCert *tls.Certificate) (*ProviderResponse, int, error) {
	externaldataRequest := NewProviderRequest(keys)
	body, err := json.Marshal(externaldataRequest)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to marshal external data request: %w", err)
	}

	client, err := defaultClientCache.getOrCreate(provider, clientCert)
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
	defer func() { _ = resp.Body.Close() }()

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

// ClientCache caches HTTP clients per provider to prevent goroutine leaks
// from creating a new transport on every request.
type ClientCache struct {
	mu      sync.Mutex
	clients map[string]*cachedClient
}

type cachedClient struct {
	client    *http.Client
	transport *http.Transport
	spec      providerSpec
	cert      atomic.Pointer[tls.Certificate]
}

// providerSpec holds the fields that affect HTTP client configuration.
// Used to detect when a provider's config has changed and the client
// needs to be recreated.
// These fields must match what getOrCreate() uses when building the HTTP client.
type providerSpec struct {
	URL      string
	Timeout  int
	CABundle string
}

func specFrom(provider *unversioned.Provider) providerSpec {
	return providerSpec{
		URL:      provider.Spec.URL,
		Timeout:  provider.Spec.Timeout,
		CABundle: provider.Spec.CABundle,
	}
}

// NewClientCache creates a new ClientCache.
func NewClientCache() *ClientCache {
	return &ClientCache{
		clients: make(map[string]*cachedClient),
	}
}

// getOrCreate returns a cached client if one exists for the provider with
// matching spec, otherwise creates a new one. Handles client cert rotation
// via atomic pointer update.
func (c *ClientCache) getOrCreate(provider *unversioned.Provider, clientCert *tls.Certificate) (*http.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	name := provider.GetName()
	spec := specFrom(provider)

	if entry, ok := c.clients[name]; ok && entry.spec == spec {
		// Always update cert atomically (including nil to clear a previously
		// configured cert) so the next TLS handshake reflects the caller's intent.
		entry.cert.Store(clientCert)
		return entry.client, nil
	}

	// Build replacement before closing old transport so validation errors don't disrupt a working client.
	entry := &cachedClient{spec: spec}
	if clientCert != nil {
		entry.cert.Store(clientCert)
	}

	// Build TLS config (same logic as getClient but with GetClientCertificate)
	u, err := url.Parse(provider.Spec.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider URL %s: %w", provider.Spec.URL, err)
	}
	if u.Scheme != HTTPSScheme {
		return nil, fmt.Errorf("only HTTPS scheme is supported")
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}

	// Always set callback so cert rotation works even if first call has no cert.
	// The callback is invoked during each TLS handshake, allowing dynamic cert updates
	// via atomic.Pointer without recreating the HTTP client.
	tlsConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		if cert := entry.cert.Load(); cert != nil {
			return cert, nil
		}
		// Return empty certificate when client auth is not configured
		return &tls.Certificate{}, nil
	}

	caBundleData, err := base64.StdEncoding.DecodeString(provider.Spec.CABundle)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA bundle: %w", err)
	}
	providerCertPool := x509.NewCertPool()
	if ok := providerCertPool.AppendCertsFromPEM(caBundleData); !ok {
		return nil, fmt.Errorf("failed to append provider's CA bundle to certificate pool")
	}
	tlsConfig.RootCAs = providerCertPool

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 10,
	}
	client := &http.Client{
		Timeout:   time.Duration(provider.Spec.Timeout) * time.Second,
		Transport: transport,
	}

	entry.client = client
	entry.transport = transport

	if old, ok := c.clients[name]; ok {
		old.transport.CloseIdleConnections()
	}
	c.clients[name] = entry

	return client, nil
}

// Invalidate closes idle connections and removes the cached client for a provider.
func (c *ClientCache) Invalidate(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.clients[name]; ok {
		entry.transport.CloseIdleConnections()
		delete(c.clients, name)
	}
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
