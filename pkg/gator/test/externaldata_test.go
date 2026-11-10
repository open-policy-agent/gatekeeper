package test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/cert"
)

const externalDataProviderName = "test-provider"

func externalDataObjects(t *testing.T) []*unstructured.Unstructured {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "..", "..", "test", "gator", "test", "fixtures", "externaldata", "policy.yaml"))
	require.NoError(t, err)
	defer file.Close()
	objs, err := reader.ReadK8sResources(file)
	require.NoError(t, err)
	require.Len(t, objs, 3)
	return objs
}

func externalDataProvider(url, caBundle string) reader.Provider {
	return reader.Provider{Resource: &unversioned.Provider{
		Spec: unversioned.ProviderSpec{
			URL:      url,
			CABundle: caBundle,
			Timeout:  3,
		},
	}}
}

func TestTestWithProviders(t *testing.T) {
	type providerResult struct {
		value       string
		itemError   string
		systemError string
	}
	var requests atomic.Int32
	var result atomic.Value
	result.Store(providerResult{value: "deny"})

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("unexpected Provider method: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request externaldata.ProviderRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decoding Provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.Request.Keys) != 1 || request.Request.Keys[0] != "tested" {
			t.Errorf("unexpected Provider keys: %v", request.Request.Keys)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		providerResponse, ok := result.Load().(providerResult)
		if !ok {
			t.Error("unexpected Provider response type")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(externaldata.ProviderResponse{
			APIVersion: "externaldata.gatekeeper.sh/v1beta1",
			Kind:       "ProviderResponse",
			Response: externaldata.Response{
				Items: []externaldata.Item{{
					Key: "tested", Value: providerResponse.value, Error: providerResponse.itemError,
				}},
				SystemError: providerResponse.systemError,
			},
		}); err != nil {
			t.Errorf("encoding Provider response: %v", err)
		}
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	ca := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	provider := externalDataProvider(server.URL, ca)
	provider.Resource.Name = externalDataProviderName
	provider.Source = "provider.yaml"
	objs := externalDataObjects(t)

	_, err := Test(objs)
	require.ErrorContains(t, err, "external_data")
	require.Zero(t, requests.Load())

	responses, err := TestWithProviders(objs, []reader.Provider{provider})
	require.NoError(t, err)
	require.Len(t, responses.Results(), 1)
	require.Equal(t, "namespace denied", responses.Results()[0].Msg)
	require.Positive(t, requests.Load())

	result.Store(providerResult{value: "allow"})
	responses, err = TestWithProviders(objs, []reader.Provider{provider})
	require.NoError(t, err)
	require.Empty(t, responses.Results())

	for _, tc := range []struct {
		response providerResult
		wantMsg  string
	}{
		{response: providerResult{systemError: "provider unavailable"}, wantMsg: "provider failure"},
		{response: providerResult{itemError: "item unavailable"}, wantMsg: "provider item error"},
	} {
		result.Store(tc.response)
		responses, err = TestWithProviders(objs, []reader.Provider{provider})
		require.NoError(t, err)
		require.Len(t, responses.Results(), 1)
		require.Equal(t, tc.wantMsg, responses.Results()[0].Msg)
	}

	// A Provider whose caBundle does not trust the server fails closed before
	// any request reaches it.
	otherCert, _, err := cert.GenerateSelfSignedCertKey("untrusted.example", nil, nil)
	require.NoError(t, err)
	untrustedCA := externalDataProvider(server.URL, base64.StdEncoding.EncodeToString(otherCert))
	untrustedCA.Resource.Name = externalDataProviderName
	before := requests.Load()
	responses, err = TestWithProviders(objs, []reader.Provider{untrustedCA})
	require.NoError(t, err)
	require.Len(t, responses.Results(), 1)
	require.Equal(t, "provider failure", responses.Results()[0].Msg)
	require.Equal(t, before, requests.Load())

	_, err = Test(objs)
	require.ErrorContains(t, err, "external_data")
	plainClient, err := gator.NewOPAClient(false)
	require.NoError(t, err)
	template, err := reader.ToTemplate(scheme, objs[0])
	require.NoError(t, err)
	_, err = plainClient.AddTemplate(context.Background(), template)
	require.ErrorContains(t, err, "external_data")
	require.Equal(t, before, requests.Load())

	untrusted := append(append([]*unstructured.Unstructured{}, objs...), &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "externaldata.gatekeeper.sh/v1beta1",
			"kind":       "Provider",
			"metadata":   map[string]interface{}{"name": "untrusted"},
		},
	})
	_, err = TestWithProviders(untrusted, []reader.Provider{provider})
	require.ErrorContains(t, err, "use --external-data-providers")
	require.Equal(t, before, requests.Load())
}

func TestTestWithProvidersErrors(t *testing.T) {
	objs := externalDataObjects(t)
	_, err := TestWithProviders(objs, nil)
	require.ErrorContains(t, err, "no external data Providers")

	_, err = TestWithProviders(objs, []reader.Provider{{Source: "nil.yaml"}})
	require.ErrorContains(t, err, "nil Provider")

	provider := externalDataProvider("http://provider.example.test", "")
	provider.Resource.Name = externalDataProviderName
	provider.Source = "invalid.yaml"
	_, err = TestWithProviders(objs, []reader.Provider{provider})
	require.ErrorContains(t, err, "invalid.yaml")
	require.ErrorContains(t, err, "HTTPS")

	provider.Resource.Spec.URL = "https://provider.example.test"
	_, err = TestWithProviders(objs, []reader.Provider{provider})
	require.ErrorContains(t, err, "caBundle")

	provider.Resource.Spec.Timeout = -1
	_, err = TestWithProviders(objs, []reader.Provider{provider})
	require.ErrorContains(t, err, "timeout")

	provider.Resource.Spec.Timeout = 3
	provider.Resource.Spec.CABundle = "not-base64"
	_, err = TestWithProviders(objs, []reader.Provider{provider})
	require.ErrorContains(t, err, "caBundle")

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	provider.Resource.Spec.CABundle = base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: server.Certificate().Raw,
	}))
	// The framework returns a missing Provider as response data, not an error.
	provider.Resource.Name = "different-provider"
	result, err := TestWithProviders(objs, []reader.Provider{provider})
	require.NoError(t, err)
	require.Len(t, result.Results(), 1)
	require.Equal(t, "provider failure", result.Results()[0].Msg)

	_, err = TestWithProviders(objs, []reader.Provider{provider, provider})
	require.ErrorContains(t, err, "already defined")
}

const externalDataExpansionObjects = `apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: imagecheck
spec:
  crd:
    spec:
      names:
        kind: ImageCheck
  targets:
  - target: admission.k8s.gatekeeper.sh
    rego: |
      package imagecheck

      violation[{"msg": msg}] {
        images := [c.image | c := input.review.object.spec.containers[_]]
        provider := ["first", "second"][_]
        response := external_data({"provider": provider, "keys": images})
        result := response.responses[_]
        result[1] != "ok"
        msg := sprintf("%v: %v is %v", [provider, result[0], result[1]])
      }
---
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: ImageCheck
metadata:
  name: check-pod-images
spec:
  enforcementAction: warn
  match:
    kinds:
    - apiGroups: [""]
      kinds: ["Pod"]
---
apiVersion: expansion.gatekeeper.sh/v1beta1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
  - groups: ["apps"]
    kinds: ["Deployment"]
    versions: ["v1"]
  templateSource: spec.template
  generatedGVK:
    group: ""
    version: v1
    kind: Pod
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  selector:
    matchLabels:
      app: app
  template:
    metadata:
      labels:
        app: app
    spec:
      containers:
      - name: good
        image: good-image
      - name: bad
        image: bad-image
`

// TestTestWithProvidersExpansion checks external data on expanded resources.
func TestTestWithProvidersExpansion(t *testing.T) {
	var mu sync.Mutex
	requestKeys := map[string][][]string{}
	newProvider := func(name string) reader.Provider {
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request externaldata.ProviderRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decoding Provider request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			requestKeys[name] = append(requestKeys[name], request.Request.Keys)
			mu.Unlock()

			items := make([]externaldata.Item, 0, len(request.Request.Keys))
			for _, key := range request.Request.Keys {
				value := "ok"
				if key == "bad-image" {
					value = name + "-denied"
				}
				items = append(items, externaldata.Item{Key: key, Value: value})
			}
			if err := json.NewEncoder(w).Encode(externaldata.ProviderResponse{
				APIVersion: "externaldata.gatekeeper.sh/v1beta1",
				Kind:       "ProviderResponse",
				Response:   externaldata.Response{Items: items},
			}); err != nil {
				t.Errorf("encoding Provider response: %v", err)
			}
		}))
		server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
		server.StartTLS()
		t.Cleanup(server.Close)

		ca := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
		provider := externalDataProvider(server.URL, ca)
		provider.Resource.Name = name
		provider.Source = name + ".yaml"
		return provider
	}

	objs, err := reader.ReadK8sResources(strings.NewReader(externalDataExpansionObjects))
	require.NoError(t, err)

	responses, err := TestWithProviders(objs, []reader.Provider{newProvider("first"), newProvider("second")})
	require.NoError(t, err)

	var got []string
	for _, result := range responses.Results() {
		require.Equal(t, "warn", result.EnforcementAction)
		require.Equal(t, "Deployment", result.ViolatingObject.GetKind())
		got = append(got, result.Msg)
	}
	require.ElementsMatch(t, []string{
		"[Implied by expand-deployments] first: bad-image is first-denied",
		"[Implied by expand-deployments] second: bad-image is second-denied",
	}, got)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requestKeys, 2)
	for name, requests := range requestKeys {
		require.Len(t, requests, 1, "Provider %s", name)
		require.ElementsMatch(t, []string{"good-image", "bad-image"}, requests[0], "Provider %s", name)
	}
}
