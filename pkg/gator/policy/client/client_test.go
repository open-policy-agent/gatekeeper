package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

const (
	v1_30_0 = "v1.30.0"
	v1_25_0 = "v1.25.0"
	v1_20_0 = "v1.20.0"
)

func TestK8sClient_ListManagedTemplates(t *testing.T) {
	// Create fake client with managed and unmanaged templates
	scheme := runtime.NewScheme()

	managedTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "managed-policy",
				"labels": map[string]interface{}{
					labels.LabelManagedBy: labels.ManagedByValue,
					labels.LabelBundle:    "test-bundle",
				},
				"annotations": map[string]interface{}{
					labels.AnnotationVersion:     "v1.2.0",
					labels.AnnotationSource:      catalog.DefaultRepository,
					labels.AnnotationInstalledAt: "2026-01-08T10:00:00Z",
				},
			},
		},
	}

	unmanagedTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "unmanaged-policy",
			},
		},
	}

	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme, managedTemplate, unmanagedTemplate)
	client := &K8sClient{dynamicClient: fakeClient}

	// List managed templates
	policies, err := client.ListManagedTemplates(context.Background())
	require.NoError(t, err)

	// Should only return managed template
	assert.Len(t, policies, 1)
	assert.Equal(t, "managed-policy", policies[0].Name)
	assert.Equal(t, "v1.2.0", policies[0].Version)
	assert.Equal(t, "test-bundle", policies[0].Bundle)
	assert.Equal(t, "2026-01-08T10:00:00Z", policies[0].InstalledAt)
}

func TestK8sClient_GetTemplate(t *testing.T) {
	scheme := runtime.NewScheme()

	template := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "test-policy",
			},
		},
	}

	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme, template)
	client := &K8sClient{dynamicClient: fakeClient}

	// Get existing template
	result, err := client.GetTemplate(context.Background(), "test-policy")
	require.NoError(t, err)
	assert.Equal(t, "test-policy", result.GetName())

	// Get non-existent template
	_, err = client.GetTemplate(context.Background(), "nonexistent")
	assert.Error(t, err)
}

// TestK8sClient_ServerVersion_Discovery exercises the real discovery-client
// request path (K8sClient.ServerVersion), which the FakeClient-based
// compatibility-gate tests never touch. It covers a successful /version
// response, request/decoding errors, and context cancellation of the
// explicit Do(ctx) call that ServerVersion relies on instead of the
// discovery client's own context-less ServerVersion().
func TestK8sClient_ServerVersion_Discovery(t *testing.T) {
	newDiscoveryK8sClient := func(t *testing.T, server *httptest.Server) *K8sClient {
		t.Helper()
		discoveryClient, err := discovery.NewDiscoveryClientForConfig(&rest.Config{Host: server.URL})
		require.NoError(t, err)
		return &K8sClient{discoveryClient: discoveryClient}
	}

	t.Run("successful gitVersion response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/version", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"gitVersion":"v1.30.2"}`))
		}))
		defer server.Close()

		client := newDiscoveryK8sClient(t, server)
		version, err := client.ServerVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "v1.30.2", version)
	})

	t.Run("response missing gitVersion is a genuine error, not a silent empty version", func(t *testing.T) {
		// Callers rely on a non-empty version for compatibility checks (see
		// resolveGateServerVersion), so a response that decodes successfully but
		// carries no gitVersion must not be allowed to flow through as ("", nil).
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()

		client := newDiscoveryK8sClient(t, server)
		_, err := client.ServerVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing gitVersion")
	})

	t.Run("server error response is a genuine error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := newDiscoveryK8sClient(t, server)
		_, err := client.ServerVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "getting server version")
	})

	t.Run("malformed response body is a genuine error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not json"))
		}))
		defer server.Close()

		client := newDiscoveryK8sClient(t, server)
		_, err := client.ServerVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing server version")
	})

	t.Run("request error (connection refused) is a genuine error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		addr := server.URL
		server.Close() // nothing is listening at addr anymore

		discoveryClient, err := discovery.NewDiscoveryClientForConfig(&rest.Config{Host: addr})
		require.NoError(t, err)
		client := &K8sClient{discoveryClient: discoveryClient}

		_, err = client.ServerVersion(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "getting server version")
	})

	t.Run("context cancellation aborts a slow request instead of blocking forever", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-release
		}))
		defer server.Close()
		defer close(release)

		client := newDiscoveryK8sClient(t, server)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := client.ServerVersion(ctx)
		elapsed := time.Since(start)

		require.Error(t, err)
		assert.Less(t, elapsed, 5*time.Second, "ServerVersion must honor context cancellation via Do(ctx) instead of blocking on the handler")
	})
}

func TestK8sClient_InstallTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme)
	client := &K8sClient{dynamicClient: fakeClient}

	template := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "new-policy",
			},
		},
	}

	// Install new template
	err := client.InstallTemplate(context.Background(), template)
	require.NoError(t, err)

	// Verify it was created
	result, err := fakeClient.Resource(ConstraintTemplateGVR).Get(context.Background(), "new-policy", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "new-policy", result.GetName())
}

func TestK8sClient_DeleteTemplate(t *testing.T) {
	scheme := runtime.NewScheme()

	template := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "to-delete",
			},
		},
	}

	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme, template)
	client := &K8sClient{dynamicClient: fakeClient}

	// Delete existing template
	err := client.DeleteTemplate(context.Background(), "to-delete")
	require.NoError(t, err)

	// Delete non-existent template should not error
	err = client.DeleteTemplate(context.Background(), "nonexistent")
	require.NoError(t, err)
}

func TestConflictError(t *testing.T) {
	err := &ConflictError{
		ResourceKind: "ConstraintTemplate",
		ResourceName: "test-policy",
	}

	assert.Contains(t, err.Error(), "ConstraintTemplate")
	assert.Contains(t, err.Error(), "test-policy")
	assert.Contains(t, err.Error(), "not managed by gator")
}

func TestGatekeeperNotInstalledError(t *testing.T) {
	err := &GatekeeperNotInstalledError{}

	assert.Contains(t, err.Error(), "Gatekeeper CRDs not found")
	assert.Contains(t, err.Error(), "https://open-policy-agent.github.io/gatekeeper/website/docs/install")
}

func TestGetConstraintResource(t *testing.T) {
	tests := []struct {
		kind     string
		expected string
	}{
		{kind: "K8sRequiredLabels", expected: "k8srequiredlabels"},
		{kind: "TestPolicy", expected: "testpolicy"},
		{kind: "", expected: ""},
	}

	for _, tt := range tests {
		result := getConstraintResource(tt.kind)
		assert.Equal(t, tt.expected, result)
	}
}

// FakeClient for testing.
type FakeClient struct {
	templates           map[string]*unstructured.Unstructured
	constraints         map[string]*unstructured.Unstructured
	gatekeeperInstalled bool
	serverVersion       string
	serverVersionErr    error
	serverVersionCalls  int
	// waitTemplateReadyErr is returned by WaitForTemplateReady, letting a test
	// simulate a Gatekeeper controller that never reconciles. The call count
	// shows how many policies a batch attempted before giving up.
	waitTemplateReadyErr   error
	waitTemplateReadyCalls int
}

func NewFakeClient() *FakeClient {
	return &FakeClient{
		templates:           make(map[string]*unstructured.Unstructured),
		constraints:         make(map[string]*unstructured.Unstructured),
		gatekeeperInstalled: true,
		serverVersion:       v1_30_0,
	}
}

func (c *FakeClient) GatekeeperInstalled(_ context.Context) (bool, error) {
	return c.gatekeeperInstalled, nil
}

func (c *FakeClient) ServerVersion(_ context.Context) (string, error) {
	c.serverVersionCalls++
	if c.serverVersionErr != nil {
		return "", c.serverVersionErr
	}
	return c.serverVersion, nil
}

func (c *FakeClient) ListManagedTemplates(_ context.Context) ([]InstalledPolicy, error) {
	var policies []InstalledPolicy
	for name, tmpl := range c.templates {
		if labels.IsManagedByGator(tmpl) {
			policies = append(policies, InstalledPolicy{
				Name:        name,
				Version:     labels.GetPolicyVersion(tmpl),
				Bundle:      labels.GetBundle(tmpl),
				InstalledAt: labels.GetInstalledAt(tmpl),
				ManagedBy:   labels.ManagedByValue,
			})
		}
	}
	return policies, nil
}

func (c *FakeClient) GetTemplate(_ context.Context, name string) (*unstructured.Unstructured, error) {
	if tmpl, ok := c.templates[name]; ok {
		return tmpl, nil
	}
	// Return a k8s-style NotFound error so uninstall logic works correctly
	return nil, k8serrors.NewNotFound(schema.GroupResource{Group: "templates.gatekeeper.sh", Resource: "constrainttemplates"}, name)
}

func (c *FakeClient) InstallTemplate(_ context.Context, template *unstructured.Unstructured) error {
	c.templates[template.GetName()] = template
	return nil
}

func (c *FakeClient) InstallConstraint(_ context.Context, constraint *unstructured.Unstructured) error {
	c.constraints[constraint.GetName()] = constraint
	return nil
}

func (c *FakeClient) GetConstraint(_ context.Context, _ schema.GroupVersionResource, name string) (*unstructured.Unstructured, error) {
	if cr, ok := c.constraints[name]; ok {
		return cr, nil
	}
	return nil, k8serrors.NewNotFound(schema.GroupResource{Group: "constraints.gatekeeper.sh", Resource: "constraints"}, name)
}

func (c *FakeClient) DeleteTemplate(_ context.Context, name string) error {
	delete(c.templates, name)
	return nil
}

func (c *FakeClient) DeleteConstraint(_ context.Context, _ schema.GroupVersionResource, name string) error {
	delete(c.constraints, name)
	return nil
}

func (c *FakeClient) WaitForTemplateReady(_ context.Context, _ string, _ time.Duration) error {
	c.waitTemplateReadyCalls++
	return c.waitTemplateReadyErr
}

func (c *FakeClient) WaitForConstraintCRD(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

// TestInstall tests the Install function.
func TestInstall(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:         "test-policy",
				Version:      "v1.0.0",
				Description:  "Test policy",
				Category:     "general",
				TemplatePath: "templates/test.yaml",
			},
		},
		Bundles: []catalog.Bundle{
			{
				Name:        "test-bundle",
				Description: "Test bundle",
				Policies:    []string{"test-policy"},
			},
		},
	}

	t.Run("install single policy", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			},
		}

		opts := &InstallOptions{
			Policies: []string{"test-policy"},
		}

		result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
		require.NoError(t, err)
		assert.Len(t, result.Installed, 1)
		assert.Equal(t, "test-policy", result.Installed[0])
		assert.Equal(t, 1, result.TemplatesInstalled)
	})

	t.Run("install non-existent policy", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fetcher := &FakeFetcher{}

		opts := &InstallOptions{
			Policies: []string{"nonexistent"},
		}

		result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
		require.NoError(t, err)
		assert.Len(t, result.Failed, 1)
		assert.Contains(t, result.Errors["nonexistent"], "not found")
	})

	t.Run("install with dry-run", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			},
		}

		opts := &InstallOptions{
			Policies: []string{"test-policy"},
			DryRun:   true,
		}

		result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
		require.NoError(t, err)
		assert.Len(t, result.Installed, 1)

		// Verify nothing was actually installed
		templates, _ := fakeClient.ListManagedTemplates(context.Background())
		assert.Empty(t, templates)
	})

	t.Run("install with no policies specified", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fetcher := &FakeFetcher{}

		opts := &InstallOptions{
			Policies: []string{},
		}

		_, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no policies specified")
	})

	t.Run("gatekeeper not installed", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.gatekeeperInstalled = false
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			},
		}

		opts := &InstallOptions{
			Policies: []string{"test-policy"},
		}

		_, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
		require.Error(t, err)
		var notInstalledErr *GatekeeperNotInstalledError
		assert.ErrorAs(t, err, &notInstalledErr)
	})
}

// TestInstall_K8sVersionCompatibility tests the cluster Kubernetes version gate.
func TestInstall_K8sVersionCompatibility(t *testing.T) {
	// versionedCatalog builds a catalog with a single policy carrying the given
	// minimum Kubernetes version bound.
	versionedCatalog := func(minVer string) *catalog.PolicyCatalog {
		return &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{
					Name:                 "versioned-policy",
					Version:              "v1.0.0",
					TemplatePath:         "templates/test.yaml",
					MinKubernetesVersion: minVer,
				},
			},
		}
	}

	newFetcher := func() *FakeFetcher {
		return &FakeFetcher{
			content: map[string][]byte{
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: versioned-policy
`),
			},
		}
	}

	t.Run("cluster below min is skipped as incompatible", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = "v1.20.5"

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Installed)
		require.Len(t, result.Incompatible, 1)
		assert.Equal(t, "versioned-policy", result.Incompatible[0].Name)
		assert.Contains(t, result.Incompatible[0].Reason, "v1.20.5")
	})

	t.Run("cluster at or above min installs", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_25_0

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Incompatible)
		assert.Len(t, result.Installed, 1)
	})

	t.Run("distro version suffix is ignored", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// Core version 1.28.3 is at or above the minimum; the "-eks.5" suffix must
		// not change the comparison.
		fakeClient.serverVersion = "v1.28.3-eks.5"

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Incompatible)
		assert.Len(t, result.Installed, 1)
	})

	t.Run("force bypasses the gate", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_20_0

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			Force:    true,
		})
		require.NoError(t, err)
		assert.Empty(t, result.Incompatible)
		assert.Len(t, result.Installed, 1)
	})

	t.Run("an unparseable cluster version is an actionable error, not fail-open", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = "not-a-version"

		// Compatibility cannot be established against a version the gate cannot
		// parse. Rather than fail open (which would install bounded policies as
		// "compatible" without ever checking), the install returns an actionable
		// error pointing at --force as the explicit opt-out.
		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "p1", Version: "v1.0.0", TemplatePath: "templates/test.yaml", MinKubernetesVersion: "v1.21.0"},
			},
		}

		_, err := Install(context.Background(), fakeClient, newFetcher(), cat, &InstallOptions{
			Policies: []string{"p1"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not-a-version")
		assert.Contains(t, err.Error(), "--force")
	})

	t.Run("force installs bounded policies despite an unparseable cluster version", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = "not-a-version"

		// --force is the single, explicit fail-open path: the gate is disabled up
		// front, so the unparseable version is never consulted and the policy
		// installs.
		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			Force:    true,
		})
		require.NoError(t, err)
		assert.Empty(t, result.Failed)
		assert.Len(t, result.Installed, 1)
	})

	t.Run("a malformed policy bound fails the affected policy", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_25_0

		// The cluster version parses fine, so the parse error can only come from
		// the policy's own bound. ParseCatalog does not run schema validation, so
		// such bad data can reach the gate via a cached/custom catalog. It must
		// fail the policy rather than fail open and install as "compatible".
		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("not-a-version"), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Installed, "a policy with an unparseable bound must not install")
		assert.Empty(t, result.Incompatible)
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "versioned-policy", result.Failed[0])
		assert.Contains(t, result.Errors["versioned-policy"], "minKubernetesVersion")
	})

	t.Run("force does not bypass a malformed policy bound", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_25_0

		// --force is documented to skip only the cluster-version compatibility
		// check. An unparseable bound is invalid policy metadata, so it must still
		// fail the policy even when forced.
		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("not-a-version"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			Force:    true,
		})
		require.NoError(t, err)
		assert.Empty(t, result.Installed, "a malformed bound must not install even with --force")
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "versioned-policy", result.Failed[0])
		assert.Contains(t, result.Errors["versioned-policy"], "minKubernetesVersion")
	})

	t.Run("a malformed bound fails only its own policy; the batch continues even under --force", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_25_0

		// The first policy carries an unparseable bound (invalid metadata,
		// satisfiable by no cluster). It must fail, but as a per-policy defect it
		// must not abort the batch: the second, valid policy must still install.
		// --force does not wave the bad bound through, yet it must not turn one bad
		// policy into a batch-wide stop either.
		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "bad", Version: "v1.0.0", TemplatePath: "templates/bad.yaml", MinKubernetesVersion: "not-a-version"},
				{Name: "good", Version: "v1.0.0", TemplatePath: "templates/good.yaml", MinKubernetesVersion: "v1.21.0"},
			},
		}
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/bad.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: bad
`),
				"templates/good.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: good
`),
			},
		}

		result, err := Install(context.Background(), fakeClient, fetcher, cat, &InstallOptions{
			Policies: []string{"bad", "good"},
			Force:    true,
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"good"}, result.Installed, "a valid policy after a defective one must still install")
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "bad", result.Failed[0])
		assert.Contains(t, result.Errors["bad"], "minKubernetesVersion")
		assert.Empty(t, result.Incompatible, "a malformed bound is invalid metadata, not a cluster incompatibility")
	})

	t.Run("idempotent reinstall on an out-of-range cluster is a no-op, not incompatible", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_20_0 // below the policy's minimum

		// Pre-install the template as gator-managed at the same version the
		// catalog offers, so a reinstall would write nothing. The compatibility
		// gate must not fire for a no-op: reinstalling an already-current policy on
		// an out-of-range cluster is not an incompatibility.
		existing := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata":   map[string]interface{}{"name": "versioned-policy"},
		}}
		labels.AddManagedLabels(existing, "v1.0.0", "", catalog.DefaultRepository)
		fakeClient.templates["versioned-policy"] = existing

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Incompatible, "an idempotent reinstall must not be reported as incompatible")
		assert.Empty(t, result.Failed)
		assert.Equal(t, []string{"versioned-policy"}, result.Skipped)
	})

	t.Run("idempotent no-op is not aborted by an unresolvable cluster version", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// The cluster version cannot be resolved at all. A bounded policy that
		// would write must fail (covered elsewhere), but an idempotent no-op needs
		// no gate: the version is resolved lazily, so a no-op never queries it and
		// must still be recorded in Skipped rather than aborting the install.
		fakeClient.serverVersionErr = errors.New("discovery unavailable")

		existing := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata":   map[string]interface{}{"name": "versioned-policy"},
		}}
		labels.AddManagedLabels(existing, "v1.0.0", "", catalog.DefaultRepository)
		fakeClient.templates["versioned-policy"] = existing

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Failed)
		assert.Empty(t, result.Incompatible)
		assert.Equal(t, []string{"versioned-policy"}, result.Skipped)
		assert.Zero(t, fakeClient.serverVersionCalls, "a no-op reinstall must not query the cluster version")
	})

	t.Run("dry-run reports unknown compatibility when the cluster version can't be checked", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// A dry-run is an offline preview: it never queries the cluster version,
		// so a bounded policy's compatibility genuinely cannot be determined. It
		// must not be previewed as installable, since a real install might reject
		// it as incompatible.
		fakeClient.serverVersion = v1_20_0 // below the policy's minimum

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			DryRun:   true,
		})
		require.NoError(t, err)
		assert.Zero(t, fakeClient.serverVersionCalls, "ServerVersion must not be queried during an offline dry-run")
		assert.Empty(t, result.Incompatible)
		assert.Empty(t, result.Installed)
		require.Len(t, result.Unknown, 1)
		assert.Equal(t, "versioned-policy", result.Unknown[0].Name)
	})

	t.Run("dry-run does not fail on an unreachable cluster", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// Even if the cluster version could not be resolved, an offline dry-run
		// never attempts to; a bounded policy is reported as unknown compatibility
		// rather than failing the whole preview.
		fakeClient.serverVersionErr = errors.New("discovery unavailable")

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			DryRun:   true,
		})
		require.NoError(t, err)
		assert.Zero(t, fakeClient.serverVersionCalls, "ServerVersion must not be queried during an offline dry-run")
		assert.Empty(t, result.Failed)
		require.Len(t, result.Unknown, 1)
		assert.Equal(t, "versioned-policy", result.Unknown[0].Name)
	})

	t.Run("dry-run with unknown compatibility still rejects a nonexistent template artifact", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// Unresolved cluster compatibility must not exempt the policy from
		// artifact validation: a bounded policy whose template can't be fetched
		// must surface as a failure, not be silently previewed as "would install"
		// under cover of "Unknown".
		emptyFetcher := &FakeFetcher{content: map[string][]byte{}}

		result, err := Install(context.Background(), fakeClient, emptyFetcher, versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			DryRun:   true,
		})
		require.NoError(t, err)
		assert.Zero(t, fakeClient.serverVersionCalls, "ServerVersion must not be queried during an offline dry-run")
		assert.Empty(t, result.Unknown)
		assert.Empty(t, result.Installed)
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "versioned-policy", result.Failed[0])
		assert.Contains(t, result.Errors["versioned-policy"], "fetching template")
	})

	t.Run("dry-run with unknown compatibility still rejects a malformed template artifact", func(t *testing.T) {
		fakeClient := NewFakeClient()
		malformedFetcher := &FakeFetcher{
			content: map[string][]byte{
				// A name mismatch is caught the same way a YAML parse error would
				// be: the artifact is fetched and validated regardless of the
				// unresolved compatibility outcome.
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: some-other-name
`),
			},
		}

		result, err := Install(context.Background(), fakeClient, malformedFetcher, versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			DryRun:   true,
		})
		require.NoError(t, err)
		assert.Empty(t, result.Unknown)
		assert.Empty(t, result.Installed)
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "versioned-policy", result.Failed[0])
		assert.Contains(t, result.Errors["versioned-policy"], "metadata.name")
	})

	t.Run("dry-run with unknown compatibility retains the note for a valid artifact", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// With a valid, matching artifact, the unknown-compatibility outcome
		// (and its explanatory note) is preserved rather than being reported as
		// installed or dropped.
		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			DryRun:   true,
		})
		require.NoError(t, err)
		assert.Empty(t, result.Failed)
		assert.Empty(t, result.Installed)
		require.Len(t, result.Unknown, 1)
		assert.Equal(t, "versioned-policy", result.Unknown[0].Name)
		assert.Contains(t, result.Unknown[0].Reason, "not verified in this offline dry-run preview")
	})

	t.Run("dry-run honors a pre-resolved version so the gate still fires", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// A caller (e.g. Upgrade) already resolved the cluster version and passes
		// it in; the dry-run preview must apply the gate without re-querying.
		fakeClient.serverVersionErr = errors.New("discovery unavailable")

		result, err := install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			DryRun:   true,
		}, v1_20_0) // below the policy's minimum
		require.NoError(t, err)
		assert.Zero(t, fakeClient.serverVersionCalls, "pre-resolved version must be reused, not re-queried")
		assert.Empty(t, result.Installed)
		require.Len(t, result.Incompatible, 1)
		assert.Contains(t, result.Incompatible[0].Reason, v1_20_0)
	})

	t.Run("dry-run with a pre-resolved version but --force bypasses the gate", func(t *testing.T) {
		fakeClient := NewFakeClient()

		result, err := install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
			DryRun:   true,
			Force:    true,
		}, v1_20_0) // below the minimum, but --force skips the check
		require.NoError(t, err)
		assert.Empty(t, result.Incompatible)
		assert.Len(t, result.Installed, 1)
	})

	t.Run("policy without bounds is always compatible", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersion = "v1.10.0"

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog(""), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Empty(t, result.Incompatible)
		assert.Len(t, result.Installed, 1)
	})

	t.Run("server version is not queried when no policy declares bounds", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// A failing ServerVersion must not break an install of unbounded policies.
		fakeClient.serverVersionErr = errors.New("discovery unavailable")

		result, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog(""), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.NoError(t, err)
		assert.Len(t, result.Installed, 1)
		assert.Zero(t, fakeClient.serverVersionCalls, "ServerVersion should not be called when no policy has bounds")
	})

	t.Run("server version query error fails the install when a policy has bounds", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fakeClient.serverVersionErr = errors.New("discovery unavailable")

		_, err := Install(context.Background(), fakeClient, newFetcher(), versionedCatalog("v1.21.0"), &InstallOptions{
			Policies: []string{"versioned-policy"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--force")
	})

	t.Run("an unresolvable cluster version fails only bounded policies; unbounded ones still install", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// The cluster version cannot be resolved at all. The bounded policy can't
		// be gated and must fail, but that must not abort the whole batch: an
		// unbounded policy in the same install still writes.
		fakeClient.serverVersionErr = errors.New("discovery unavailable")

		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "bounded", Version: "v1.0.0", TemplatePath: "templates/bounded.yaml", MinKubernetesVersion: "v1.21.0"},
				{Name: "unbounded", Version: "v1.0.0", TemplatePath: "templates/unbounded.yaml"},
			},
		}
		// Each template artifact's metadata.name must match its catalog policy
		// name (installPolicy rejects a mismatch to preserve conflict protection).
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/bounded.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: bounded
`),
				"templates/unbounded.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: unbounded
`),
			},
		}

		result, err := Install(context.Background(), fakeClient, fetcher, cat, &InstallOptions{
			Policies: []string{"bounded", "unbounded"},
		})
		// The partial result is returned alongside the resolution error (with the
		// --force hint), never discarded.
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--force")
		require.NotNil(t, result)
		assert.Equal(t, []string{"unbounded"}, result.Installed, "unbounded policy must still install")
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "bounded", result.Failed[0])
		assert.Contains(t, result.Errors["bounded"], "--force")
	})

	t.Run("a not-found policy folds its own error into a pending cluster-version error", func(t *testing.T) {
		// "bounded" fails cluster-version resolution, then "typo" is not in the
		// catalog and trips the fail-fast branch. Both causes must reach the
		// caller: the version error drives the exit code (the cluster is
		// unreachable) and the not-found detail rides along in the message
		// rather than being dropped.
		fakeClient := NewFakeClient()
		fakeClient.serverVersionErr = errors.New("discovery unavailable")

		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "bounded", Version: "v1.0.0", TemplatePath: "templates/bounded.yaml", MinKubernetesVersion: "v1.21.0"},
			},
		}
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/bounded.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: bounded
`),
			},
		}

		result, err := Install(context.Background(), fakeClient, fetcher, cat, &InstallOptions{
			Policies: []string{"bounded", "typo"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--force", "the cluster-version cause must stay first so the exit code is a cluster error")
		assert.Contains(t, err.Error(), "policy not found: typo", "the not-found cause must not be dropped")
		require.NotNil(t, result)
		assert.Equal(t, []string{"bounded", "typo"}, result.Failed)
		assert.Contains(t, result.Errors["typo"], "policy not found: typo")
	})

	t.Run("a not-found policy alone is reported through the result, not a top-level error", func(t *testing.T) {
		// With no pending cluster-version error, fail-fast returns a nil error so
		// the caller classifies the failure from result.Failed (exit 4), rather
		// than a cluster error.
		fakeClient := NewFakeClient()
		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{{Name: "known", Version: "v1.0.0", TemplatePath: "templates/known.yaml"}},
		}
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/known.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: known
`),
			},
		}

		result, err := Install(context.Background(), fakeClient, fetcher, cat, &InstallOptions{
			Policies: []string{"known", "typo"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"known"}, result.Installed)
		assert.Equal(t, []string{"typo"}, result.Failed)
		assert.Contains(t, result.Errors["typo"], "policy not found: typo")
	})
}

// TestUninstall tests the Uninstall function.
func TestUninstall(t *testing.T) {
	t.Run("uninstall managed policy", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// Add a managed template (requires both label AND annotation)
		tmpl := &unstructured.Unstructured{}
		tmpl.SetName("test-policy")
		tmpl.SetLabels(map[string]string{
			labels.LabelManagedBy: labels.ManagedByValue,
		})
		tmpl.SetAnnotations(map[string]string{
			labels.AnnotationSource: catalog.DefaultRepository,
		})
		fakeClient.templates["test-policy"] = tmpl

		opts := UninstallOptions{
			Policies: []string{"test-policy"},
		}

		result, err := Uninstall(context.Background(), fakeClient, opts)
		require.NoError(t, err)
		assert.Len(t, result.Uninstalled, 1)
		assert.Equal(t, "test-policy", result.Uninstalled[0])
	})

	t.Run("uninstall non-existent policy", func(t *testing.T) {
		fakeClient := NewFakeClient()

		opts := UninstallOptions{
			Policies: []string{"nonexistent"},
		}

		result, err := Uninstall(context.Background(), fakeClient, opts)
		require.NoError(t, err)
		assert.Len(t, result.NotFound, 1)
		assert.Equal(t, "nonexistent", result.NotFound[0])
	})

	t.Run("uninstall unmanaged policy", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// Add an unmanaged template
		tmpl := &unstructured.Unstructured{}
		tmpl.SetName("unmanaged-policy")
		fakeClient.templates["unmanaged-policy"] = tmpl

		opts := UninstallOptions{
			Policies: []string{"unmanaged-policy"},
		}

		result, err := Uninstall(context.Background(), fakeClient, opts)
		// Conflict errors are non-fatal — tracked in NotManaged, not Failed
		require.NoError(t, err)
		assert.Len(t, result.NotManaged, 1)
		assert.Empty(t, result.Failed)
		assert.Contains(t, result.Errors["unmanaged-policy"], "ConstraintTemplate")
	})

	t.Run("uninstall with no policies", func(t *testing.T) {
		fakeClient := NewFakeClient()

		opts := UninstallOptions{
			Policies: []string{},
		}

		_, err := Uninstall(context.Background(), fakeClient, opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no policies specified")
	})
}

// TestUpgrade tests the Upgrade function.
func TestUpgrade(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:         "test-policy",
				Version:      "v2.0.0",
				Description:  "Updated policy",
				Category:     "general",
				TemplatePath: "templates/test.yaml",
			},
		},
	}

	t.Run("upgrade with --all", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// Add a managed template with old version
		tmpl := &unstructured.Unstructured{}
		tmpl.SetName("test-policy")
		tmpl.SetLabels(map[string]string{
			labels.LabelManagedBy: labels.ManagedByValue,
		})
		tmpl.SetAnnotations(map[string]string{
			labels.AnnotationVersion: "v1.0.0",
			labels.AnnotationSource:  catalog.DefaultRepository,
		})
		fakeClient.templates["test-policy"] = tmpl

		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			},
		}

		opts := UpgradeOptions{
			All: true,
		}

		result, err := Upgrade(context.Background(), fakeClient, fetcher, cat, opts)
		require.NoError(t, err)
		assert.Len(t, result.Upgraded, 1)
		assert.Equal(t, "v1.0.0", result.Upgraded[0].FromVersion)
		assert.Equal(t, "v2.0.0", result.Upgraded[0].ToVersion)
	})

	t.Run("upgrade already current", func(t *testing.T) {
		fakeClient := NewFakeClient()
		// Add a managed template with same version as catalog
		tmpl := &unstructured.Unstructured{}
		tmpl.SetName("test-policy")
		tmpl.SetLabels(map[string]string{
			labels.LabelManagedBy: labels.ManagedByValue,
		})
		tmpl.SetAnnotations(map[string]string{
			labels.AnnotationVersion: "v2.0.0",
			labels.AnnotationSource:  catalog.DefaultRepository,
		})
		fakeClient.templates["test-policy"] = tmpl

		fetcher := &FakeFetcher{}

		opts := UpgradeOptions{
			All: true,
		}

		result, err := Upgrade(context.Background(), fakeClient, fetcher, cat, opts)
		require.NoError(t, err)
		assert.Len(t, result.AlreadyCurrent, 1)
		assert.Empty(t, result.Upgraded)
	})

	t.Run("upgrade without --all or policies", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fetcher := &FakeFetcher{}

		opts := UpgradeOptions{}

		_, err := Upgrade(context.Background(), fakeClient, fetcher, cat, opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--all")
	})

	t.Run("upgrade policy not installed", func(t *testing.T) {
		fakeClient := NewFakeClient()
		fetcher := &FakeFetcher{}

		opts := UpgradeOptions{
			Policies: []string{"test-policy"},
		}

		result, err := Upgrade(context.Background(), fakeClient, fetcher, cat, opts)
		require.NoError(t, err)
		assert.Len(t, result.NotInstalled, 1)
	})

	// managedTemplateAt builds a gator-managed ConstraintTemplate installed at
	// the given version.
	managedTemplateAt := func(name, version string) *unstructured.Unstructured {
		tmpl := &unstructured.Unstructured{}
		tmpl.SetName(name)
		tmpl.SetLabels(map[string]string{labels.LabelManagedBy: labels.ManagedByValue})
		tmpl.SetAnnotations(map[string]string{
			labels.AnnotationVersion: version,
			labels.AnnotationSource:  catalog.DefaultRepository,
		})
		return tmpl
	}

	t.Run("upgrade to k8s-incompatible version is skipped as incompatible, not upgraded", func(t *testing.T) {
		boundedCat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{
					Name:                 "test-policy",
					Version:              "v2.0.0",
					TemplatePath:         "templates/test.yaml",
					MinKubernetesVersion: "v1.31.0",
				},
			},
		}

		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_30_0 // below the policy's minimum
		fakeClient.templates["test-policy"] = managedTemplateAt("test-policy", "v1.0.0")

		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			},
		}

		result, err := Upgrade(context.Background(), fakeClient, fetcher, boundedCat, UpgradeOptions{All: true})
		require.NoError(t, err)
		assert.Empty(t, result.Upgraded, "incompatible policy must not be reported as upgraded")
		assert.Empty(t, result.Failed, "incompatible is a skip, not a hard failure")
		require.Len(t, result.Incompatible, 1)
		assert.Equal(t, "test-policy", result.Incompatible[0].Name)
		assert.Contains(t, result.Incompatible[0].Reason, v1_30_0)

		// --force bypasses the gate and completes the upgrade.
		fakeClient2 := NewFakeClient()
		fakeClient2.serverVersion = v1_30_0
		fakeClient2.templates["test-policy"] = managedTemplateAt("test-policy", "v1.0.0")
		result2, err := Upgrade(context.Background(), fakeClient2, fetcher, boundedCat, UpgradeOptions{All: true, Force: true})
		require.NoError(t, err)
		assert.Len(t, result2.Upgraded, 1)
		assert.Empty(t, result2.Incompatible)
	})

	t.Run("upgrade --all continues past an incompatible policy and fetches version once", func(t *testing.T) {
		// "incompatible" needs v1.31; "compatible" is unbounded and has an update.
		mixedCat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "incompatible", Version: "v2.0.0", TemplatePath: "templates/incompatible.yaml", MinKubernetesVersion: "v1.31.0"},
				{Name: "compatible", Version: "v2.0.0", TemplatePath: "templates/compatible.yaml"},
			},
		}

		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_30_0 // below "incompatible"'s minimum
		// Processing order is non-deterministic (map iteration); with skip-continue
		// the outcome must not depend on it — the incompatible policy never blocks
		// the compatible one, whichever comes first.
		fakeClient.templates["incompatible"] = managedTemplateAt("incompatible", "v1.0.0")
		fakeClient.templates["compatible"] = managedTemplateAt("compatible", "v1.0.0")

		// Each template artifact's metadata.name must match its catalog policy name.
		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/incompatible.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: incompatible
`),
				"templates/compatible.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: compatible
`),
			},
		}

		result, err := Upgrade(context.Background(), fakeClient, fetcher, mixedCat, UpgradeOptions{All: true})
		require.NoError(t, err)
		// The compatible policy is still upgraded despite the incompatible one.
		require.Len(t, result.Upgraded, 1)
		assert.Equal(t, "compatible", result.Upgraded[0].Name)
		require.Len(t, result.Incompatible, 1)
		assert.Equal(t, "incompatible", result.Incompatible[0].Name)
		assert.Empty(t, result.Failed)
		// The cluster version is resolved once for the whole batch, not per policy.
		assert.Equal(t, 1, fakeClient.serverVersionCalls)
	})

	t.Run("upgrade records a genuine per-policy error without aborting the batch", func(t *testing.T) {
		// "broken" has no fetchable template so its upgrade errors; "healthy"
		// comes after it in the explicit policy order. A failure scoped to one
		// policy must not prevent the remaining candidates from being attempted.
		mixedCat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "broken", Version: "v2.0.0", TemplatePath: "templates/missing.yaml"},
				{Name: "healthy", Version: "v2.0.0", TemplatePath: "templates/t.yaml"},
			},
		}

		fakeClient := NewFakeClient()
		fakeClient.templates["broken"] = managedTemplateAt("broken", "v1.0.0")
		fakeClient.templates["healthy"] = managedTemplateAt("healthy", "v1.0.0")

		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/t.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: healthy
`),
			},
		}

		opts := UpgradeOptions{Policies: []string{"broken", "healthy"}}
		result, err := Upgrade(context.Background(), fakeClient, fetcher, mixedCat, opts)
		require.NoError(t, err)
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "broken", result.Failed[0])
		// "healthy" was still attempted and upgraded despite "broken" failing.
		require.Len(t, result.Upgraded, 1)
		assert.Equal(t, "healthy", result.Upgraded[0].Name)
	})

	// bundledCat holds two policies that both carry a bundle constraint, so
	// upgrading either one goes through installConstraint (and therefore the
	// reconcile waits).
	bundledCat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{Name: "first", Version: "v2.0.0", TemplatePath: "templates/first.yaml", BundleConstraints: map[string]string{"b": "constraints/first.yaml"}},
			{Name: "second", Version: "v2.0.0", TemplatePath: "templates/second.yaml", BundleConstraints: map[string]string{"b": "constraints/second.yaml"}},
		},
		Bundles: []catalog.Bundle{{Name: "b", Policies: []string{"first", "second"}}},
	}

	// bundledFetcher serves a template and a constraint for each bundledCat policy.
	bundledFetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/first.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: first
`),
			"constraints/first.yaml": []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: First
metadata:
  name: first-constraint
`),
			"templates/second.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: second
`),
			"constraints/second.yaml": []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: Second
metadata:
  name: second-constraint
`),
		},
	}

	// bundledTemplateAt builds a gator-managed ConstraintTemplate that records
	// the bundle it was installed from, so Upgrade re-applies its constraint.
	bundledTemplateAt := func(name, version string) *unstructured.Unstructured {
		tmpl := managedTemplateAt(name, version)
		tmplLabels := tmpl.GetLabels()
		tmplLabels[labels.LabelBundle] = "b"
		tmpl.SetLabels(tmplLabels)
		return tmpl
	}

	t.Run("upgrade aborts the batch on a cluster-scoped failure instead of retrying every policy", func(t *testing.T) {
		// A Gatekeeper controller that never marks the template ready is a
		// property of the cluster, not of any one policy: every remaining
		// candidate would wait out DefaultReconcileTimeout for the same reason.
		fakeClient := NewFakeClient()
		fakeClient.templates["first"] = bundledTemplateAt("first", "v1.0.0")
		fakeClient.templates["second"] = bundledTemplateAt("second", "v1.0.0")
		fakeClient.waitTemplateReadyErr = &ReconcileTimeoutError{Resource: `template "first" to be ready`}

		opts := UpgradeOptions{Policies: []string{"first", "second"}}
		result, err := Upgrade(context.Background(), fakeClient, bundledFetcher, bundledCat, opts)

		require.Error(t, err, "a cluster-scoped failure must surface as a top-level error")
		var timeoutErr *ReconcileTimeoutError
		assert.ErrorAs(t, err, &timeoutErr, "the typed cause must survive the trip through install")
		assert.Empty(t, result.Upgraded)
		// Only the first candidate was attempted; the batch did not wait out the
		// same timeout again for "second".
		assert.Len(t, result.Failed, 1)
		assert.Equal(t, 1, fakeClient.waitTemplateReadyCalls)
	})

	t.Run("upgrade records a conflict, continues the batch, and keeps it classifiable", func(t *testing.T) {
		// "first"'s constraint already exists and is not gator-managed. That is
		// an ownership conflict scoped to one policy, so "second" still upgrades,
		// but the conflict must stay distinguishable from a generic failure.
		fakeClient := NewFakeClient()
		fakeClient.templates["first"] = bundledTemplateAt("first", "v1.0.0")
		fakeClient.templates["second"] = bundledTemplateAt("second", "v1.0.0")
		unmanaged := &unstructured.Unstructured{}
		unmanaged.SetName("first-constraint")
		unmanaged.SetKind("First")
		fakeClient.constraints["first-constraint"] = unmanaged

		opts := UpgradeOptions{Policies: []string{"first", "second"}}
		result, err := Upgrade(context.Background(), fakeClient, bundledFetcher, bundledCat, opts)

		// A conflict is per-policy, so it is not a top-level error.
		require.NoError(t, err)
		require.Len(t, result.Failed, 1)
		assert.Equal(t, "first", result.Failed[0])
		require.NotNil(t, result.ConflictErr, "the conflict must be reported so the CLI can exit 3")
		assert.Equal(t, "First", result.ConflictErr.ResourceKind)
		assert.Equal(t, "first-constraint", result.ConflictErr.ResourceName)
		assert.Contains(t, result.Errors["first"], "not managed by gator")
		// "second" was still attempted and upgraded.
		require.Len(t, result.Upgraded, 1)
		assert.Equal(t, "second", result.Upgraded[0].Name)
	})

	t.Run("dry-run applies the compatibility gate so the preview matches a real run", func(t *testing.T) {
		boundedCat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{
					Name:                 "test-policy",
					Version:              "v2.0.0",
					TemplatePath:         "templates/test.yaml",
					MinKubernetesVersion: "v1.31.0",
				},
			},
		}

		fakeClient := NewFakeClient()
		fakeClient.serverVersion = v1_30_0 // below the policy's minimum
		fakeClient.templates["test-policy"] = managedTemplateAt("test-policy", "v1.0.0")

		fetcher := &FakeFetcher{
			content: map[string][]byte{
				"templates/test.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			},
		}

		result, err := Upgrade(context.Background(), fakeClient, fetcher, boundedCat, UpgradeOptions{All: true, DryRun: true})
		require.NoError(t, err)
		// Dry-run must not preview an incompatible policy as "Would upgrade".
		assert.Empty(t, result.Upgraded, "incompatible policy must not be previewed as upgraded")
		require.Len(t, result.Incompatible, 1)
		assert.Equal(t, "test-policy", result.Incompatible[0].Name)
		assert.Contains(t, result.Incompatible[0].Reason, v1_30_0)
	})
}

func TestGetUpgradableCount(t *testing.T) {
	installed := []InstalledPolicy{
		{Name: "policy1", Version: "v1.0.0"},
		{Name: "policy2", Version: "v2.0.0"},
		{Name: "policy3", Version: "v1.0.0"},
	}

	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{Name: "policy1", Version: "v2.0.0"}, // upgradable
			{Name: "policy2", Version: "v2.0.0"}, // current
			{Name: "policy3", Version: "v1.5.0"}, // upgradable
		},
	}

	count := GetUpgradableCount(installed, cat, "")
	assert.Equal(t, 2, count)
}

func TestGetUpgradablePolicies(t *testing.T) {
	installed := []InstalledPolicy{
		{Name: "policy1", Version: "v1.0.0"},
		{Name: "policy2", Version: "v2.0.0"},
	}

	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{Name: "policy1", Version: "v2.0.0"},
			{Name: "policy2", Version: "v2.0.0"},
		},
	}

	changes := GetUpgradablePolicies(installed, cat, "")
	assert.Len(t, changes, 1)
	assert.Equal(t, "policy1", changes[0].Name)
	assert.Equal(t, "v1.0.0", changes[0].FromVersion)
	assert.Equal(t, "v2.0.0", changes[0].ToVersion)
}

func TestGetUpgradablePolicies_K8sVersionGate(t *testing.T) {
	installed := []InstalledPolicy{
		{Name: "compatible", Version: "v1.0.0"},
		{Name: "incompatible", Version: "v1.0.0"},
	}
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{Name: "compatible", Version: "v2.0.0"},
			{Name: "incompatible", Version: "v2.0.0", MinKubernetesVersion: "v1.31.0"},
		},
	}

	t.Run("incompatible upgrade is excluded when cluster version is known", func(t *testing.T) {
		// Cluster is below "incompatible"'s minimum, so upgrade would skip it; it
		// must not be advertised as upgradable.
		changes := GetUpgradablePolicies(installed, cat, v1_30_0)
		require.Len(t, changes, 1)
		assert.Equal(t, "compatible", changes[0].Name)
		assert.Equal(t, 1, GetUpgradableCount(installed, cat, v1_30_0))
	})

	t.Run("unknown cluster version does not hide upgrades", func(t *testing.T) {
		// With no cluster version, the gate is skipped and both upgrades are
		// reported rather than silently dropped.
		assert.Equal(t, 2, GetUpgradableCount(installed, cat, ""))
	})

	t.Run("cluster within range keeps the upgrade", func(t *testing.T) {
		assert.Equal(t, 2, GetUpgradableCount(installed, cat, "v1.31.2"))
	})

	t.Run("unparseable version bound is excluded, matching Upgrade's fail-closed handling", func(t *testing.T) {
		malformed := []InstalledPolicy{{Name: "malformed", Version: "v1.0.0"}}
		malformedCat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "malformed", Version: "v2.0.0", MinKubernetesVersion: "not-a-version"},
			},
		}
		changes := GetUpgradablePolicies(malformed, malformedCat, v1_30_0)
		assert.Empty(t, changes)
		assert.Equal(t, 0, GetUpgradableCount(malformed, malformedCat, v1_30_0))
	})

	t.Run("unparseable version bound is excluded even when cluster version is unknown", func(t *testing.T) {
		// A malformed bound is invalid metadata regardless of the cluster's
		// version, so Upgrade fails it outright. It must not be advertised as
		// upgradable just because the cluster version could not be resolved.
		malformed := []InstalledPolicy{{Name: "malformed", Version: "v1.0.0"}}
		malformedCat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{
				{Name: "malformed", Version: "v2.0.0", MinKubernetesVersion: "not-a-version"},
			},
		}
		changes := GetUpgradablePolicies(malformed, malformedCat, "")
		assert.Empty(t, changes)
		assert.Equal(t, 0, GetUpgradableCount(malformed, malformedCat, ""))
	})
}

func TestPolicyNeedsVersionGate(t *testing.T) {
	t.Run("false when no upgrade candidate has version bounds", func(t *testing.T) {
		installed := []InstalledPolicy{{Name: "foo", Version: "v1.0.0"}}
		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{{Name: "foo", Version: "v2.0.0"}},
		}
		assert.False(t, PolicyNeedsVersionGate(installed, cat))
	})

	t.Run("false when the only bounded policy is already current", func(t *testing.T) {
		installed := []InstalledPolicy{{Name: "foo", Version: "v2.0.0"}}
		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{{Name: "foo", Version: "v2.0.0", MinKubernetesVersion: "v1.31.0"}},
		}
		assert.False(t, PolicyNeedsVersionGate(installed, cat))
	})

	t.Run("true when an upgrade candidate has version bounds", func(t *testing.T) {
		installed := []InstalledPolicy{{Name: "foo", Version: "v1.0.0"}}
		cat := &catalog.PolicyCatalog{
			Policies: []catalog.Policy{{Name: "foo", Version: "v2.0.0", MinKubernetesVersion: "v1.31.0"}},
		}
		assert.True(t, PolicyNeedsVersionGate(installed, cat))
	})
}

// FakeFetcher is a test implementation of catalog.Fetcher.
type FakeFetcher struct {
	content map[string][]byte
}

func (f *FakeFetcher) Fetch(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (f *FakeFetcher) FetchContent(_ context.Context, path string) ([]byte, error) {
	if data, ok := f.content[path]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("content not found: %s", path)
}

func (f *FakeFetcher) SetInsecure(_ bool) {}

// TestInstallBundlePlusPositionalPolicies tests that bundle + positional policies are both installed.
func TestInstallBundlePlusPositionalPolicies(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:              "bundle-policy",
				Version:           "v1.0.0",
				Description:       "Policy in bundle",
				Category:          "general",
				TemplatePath:      "templates/bundle-policy.yaml",
				BundleConstraints: map[string]string{"test-bundle": "constraints/bundle-policy.yaml"},
			},
			{
				Name:         "additional-policy",
				Version:      "v1.0.0",
				Description:  "Additional policy",
				Category:     "general",
				TemplatePath: "templates/additional-policy.yaml",
			},
		},
		Bundles: []catalog.Bundle{
			{
				Name:        "test-bundle",
				Description: "Test bundle",
				Policies:    []string{"bundle-policy"},
			},
		},
	}

	fakeClient := NewFakeClient()
	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/bundle-policy.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: bundle-policy
`),
			"constraints/bundle-policy.yaml": []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: BundlePolicy
metadata:
  name: bundle-policy-constraint
`),
			"templates/additional-policy.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: additional-policy
`),
		},
	}

	// Install bundle AND additional policy
	opts := &InstallOptions{
		Policies: []string{"additional-policy"},
		Bundles:  []string{"test-bundle"},
	}

	result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
	require.NoError(t, err)

	// Both should be installed
	assert.Len(t, result.Installed, 2)
	assert.Contains(t, result.Installed, "bundle-policy")
	assert.Contains(t, result.Installed, "additional-policy")

	// Bundle policy should have constraint installed
	assert.Equal(t, 1, result.ConstraintsInstalled)

	// Templates installed should be 2
	assert.Equal(t, 2, result.TemplatesInstalled)
}

// TestUpgradeWithBundleContext tests that upgrade preserves bundle context for constraints.
func TestUpgradeWithBundleContext(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:              "bundle-policy",
				Version:           "v2.0.0",
				Description:       "Updated bundle policy",
				Category:          "general",
				TemplatePath:      "templates/bundle-policy.yaml",
				BundleConstraints: map[string]string{"test-bundle": "constraints/bundle-policy.yaml"},
			},
		},
		Bundles: []catalog.Bundle{
			{
				Name:        "test-bundle",
				Description: "Test bundle",
				Policies:    []string{"bundle-policy"},
			},
		},
	}

	fakeClient := NewFakeClient()
	// Add existing managed template with bundle label
	tmpl := &unstructured.Unstructured{}
	tmpl.SetName("bundle-policy")
	tmpl.SetLabels(map[string]string{
		labels.LabelManagedBy: labels.ManagedByValue,
		labels.LabelBundle:    "test-bundle",
	})
	tmpl.SetAnnotations(map[string]string{
		labels.AnnotationVersion: "v1.0.0",
		labels.AnnotationSource:  catalog.DefaultRepository,
	})
	fakeClient.templates["bundle-policy"] = tmpl

	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/bundle-policy.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: bundle-policy
`),
			"constraints/bundle-policy.yaml": []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: BundlePolicy
metadata:
  name: bundle-policy-constraint
`),
		},
	}

	opts := UpgradeOptions{
		Policies: []string{"bundle-policy"},
	}

	result, err := Upgrade(context.Background(), fakeClient, fetcher, cat, opts)
	require.NoError(t, err)
	assert.Len(t, result.Upgraded, 1)
	assert.Equal(t, "v1.0.0", result.Upgraded[0].FromVersion)
	assert.Equal(t, "v2.0.0", result.Upgraded[0].ToVersion)
}

func TestInstallSetEnforcementAction(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:              "test-policy",
				Version:           "v1.0.0",
				TemplatePath:      "templates/test.yaml",
				BundleConstraints: map[string]string{"test-bundle": "constraints/test.yaml"},
			},
		},
		Bundles: []catalog.Bundle{
			{Name: "test-bundle", Policies: []string{"test-policy"}},
		},
	}

	fakeClient := NewFakeClient()
	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/test.yaml": []byte(`apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			"constraints/test.yaml": []byte(`apiVersion: constraints.gatekeeper.sh/v1beta1
kind: TestPolicy
metadata:
  name: test-constraint
spec:
  enforcementAction: deny
`),
		},
	}

	opts := &InstallOptions{
		Bundles:           []string{"test-bundle"},
		EnforcementAction: "warn",
	}

	result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
	require.NoError(t, err)
	assert.Len(t, result.Installed, 1)
	assert.Equal(t, 1, result.ConstraintsInstalled)

	// Verify constraint has overridden enforcement action
	constraint := fakeClient.constraints["test-constraint"]
	require.NotNil(t, constraint)
	action, _, _ := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
	assert.Equal(t, "warn", action)
}

// TestInstall_IncompatibleWithUnmanagedConflictDoesNotBlockBatch locks in the
// ordering inside installPolicy between the Kubernetes-version compatibility
// gate and the ownership-conflict check: an incompatible policy whose target
// template collides with a pre-existing, unmanaged ConstraintTemplate must
// resolve as Incompatible, not a ConflictError. Incompatible is non-fatal, so
// a second, compatible policy in the same Install() call must still install —
// whereas a ConflictError would abort the whole batch (see install()'s
// "fail fast" loop) and block it.
func TestInstall_IncompatibleWithUnmanagedConflictDoesNotBlockBatch(t *testing.T) {
	fakeClient := NewFakeClient()
	fakeClient.serverVersion = v1_20_0 // below incompatible-policy's minimum

	// A pre-existing, unmanaged (user-owned) ConstraintTemplate already
	// occupies the name "incompatible-policy" would install to.
	unmanaged := &unstructured.Unstructured{}
	unmanaged.SetName("incompatible-policy")
	unmanaged.SetAnnotations(map[string]string{"owner": "user"})
	fakeClient.templates["incompatible-policy"] = unmanaged

	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:                 "incompatible-policy",
				Version:              "v1.0.0",
				TemplatePath:         "templates/incompatible.yaml",
				MinKubernetesVersion: "v1.21.0",
			},
			{
				Name:         "compatible-policy",
				Version:      "v1.0.0",
				TemplatePath: "templates/compatible.yaml",
			},
		},
	}

	// The incompatible policy's template is deliberately absent from the
	// fetcher: the compatibility gate must reject it before the artifact is
	// ever fetched. If the ordering regressed and it were fetched, the test
	// would fail loudly (as a Failed policy) rather than passing accidentally.
	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/compatible.yaml": []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: compatible-policy
`),
		},
	}

	result, err := Install(context.Background(), fakeClient, fetcher, cat, &InstallOptions{
		Policies: []string{"incompatible-policy", "compatible-policy"},
	})
	require.NoError(t, err)

	require.Len(t, result.Incompatible, 1)
	assert.Equal(t, "incompatible-policy", result.Incompatible[0].Name)
	assert.Nil(t, result.ConflictErr)
	assert.Empty(t, result.Failed)

	// Non-fatal outcome: the batch continues and the compatible policy installs.
	assert.Equal(t, []string{"compatible-policy"}, result.Installed)

	// The pre-existing unmanaged template is completely untouched.
	got := fakeClient.templates["incompatible-policy"]
	require.NotNil(t, got)
	assert.False(t, labels.IsManagedByGator(got), "unmanaged template must not be relabeled")
	assert.Equal(t, "user", got.GetAnnotations()["owner"], "unmanaged template must not be overwritten")
}

func TestInstallConstraintConflict(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:              "test-policy",
				Version:           "v1.0.0",
				TemplatePath:      "templates/test.yaml",
				BundleConstraints: map[string]string{"test-bundle": "constraints/test.yaml"},
			},
		},
		Bundles: []catalog.Bundle{
			{Name: "test-bundle", Policies: []string{"test-policy"}},
		},
	}

	fakeClient := NewFakeClient()
	// Pre-install an unmanaged constraint (no gator labels)
	fakeClient.constraints["test-constraint"] = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestPolicy",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
			},
		},
	}

	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/test.yaml": []byte(`apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			"constraints/test.yaml": []byte(`apiVersion: constraints.gatekeeper.sh/v1beta1
kind: TestPolicy
metadata:
  name: test-constraint
`),
		},
	}

	opts := &InstallOptions{
		Bundles: []string{"test-bundle"},
	}

	result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
	require.NoError(t, err) // Install returns result with failure, not error
	assert.NotNil(t, result.ConflictErr)
	assert.Len(t, result.Failed, 1)
}

// TestInstallTemplateNameMismatchDoesNotOverwrite guards the conflict-protection
// bypass: the preflight conflict check keys off policy.Name, but InstallTemplate
// writes to the artifact's metadata.name. If a catalog entry ("alias") points to
// a template whose metadata.name ("real") is a pre-existing, unmanaged
// ConstraintTemplate, install must reject the mismatch rather than silently
// overwrite the user-owned template.
func TestInstallTemplateNameMismatchDoesNotOverwrite(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{Name: "alias", Version: "v1.0.0", TemplatePath: "templates/alias.yaml"},
		},
	}

	fakeClient := NewFakeClient()
	// A user-owned (unmanaged) ConstraintTemplate named "real".
	userTemplate := &unstructured.Unstructured{}
	userTemplate.SetName("real")
	userTemplate.SetAnnotations(map[string]string{"owner": "user"})
	fakeClient.templates["real"] = userTemplate

	// The catalog entry is named "alias" but its artifact declares metadata.name
	// "real" — pointing at the user-owned template.
	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/alias.yaml": []byte(`apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: real
`),
		},
	}

	result, err := Install(context.Background(), fakeClient, fetcher, cat, &InstallOptions{
		Policies: []string{"alias"},
	})
	require.NoError(t, err) // per-policy failures are captured in the result
	assert.Empty(t, result.Installed)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, "alias", result.Failed[0])
	assert.Contains(t, result.Errors["alias"], "metadata.name")

	// The user-owned template must be untouched: same object, no gator labels.
	got := fakeClient.templates["real"]
	require.NotNil(t, got)
	assert.False(t, labels.IsManagedByGator(got), "user-owned template must not be relabeled")
	assert.Equal(t, "user", got.GetAnnotations()["owner"], "user-owned template must not be overwritten")
}

func TestInstallPreservesExistingEnforcementAction(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:              "test-policy",
				Version:           "v2.0.0",
				TemplatePath:      "templates/test.yaml",
				BundleConstraints: map[string]string{"test-bundle": "constraints/test.yaml"},
			},
		},
		Bundles: []catalog.Bundle{
			{Name: "test-bundle", Policies: []string{"test-policy"}},
		},
	}

	fakeClient := NewFakeClient()

	// Pre-install a managed constraint with "warn" enforcement
	managedConstraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestPolicy",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"labels": map[string]interface{}{
					labels.LabelManagedBy: labels.ManagedByValue,
				},
				"annotations": map[string]interface{}{
					labels.AnnotationSource: catalog.DefaultRepository,
				},
			},
			"spec": map[string]interface{}{
				"enforcementAction": "warn",
			},
		},
	}
	fakeClient.constraints["test-constraint"] = managedConstraint

	// Pre-install the template as managed at v1.0.0 (so it's treated as an upgrade)
	tmpl := &unstructured.Unstructured{}
	tmpl.SetName("test-policy")
	tmpl.SetLabels(map[string]string{
		labels.LabelManagedBy: labels.ManagedByValue,
	})
	tmpl.SetAnnotations(map[string]string{
		labels.AnnotationVersion: "v1.0.0",
		labels.AnnotationSource:  catalog.DefaultRepository,
	})
	fakeClient.templates["test-policy"] = tmpl

	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/test.yaml": []byte(`apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-policy
`),
			"constraints/test.yaml": []byte(`apiVersion: constraints.gatekeeper.sh/v1beta1
kind: TestPolicy
metadata:
  name: test-constraint
spec:
  enforcementAction: deny
`),
		},
	}

	// Install WITHOUT specifying enforcement action — should preserve "warn"
	opts := &InstallOptions{
		Bundles: []string{"test-bundle"},
	}

	result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
	require.NoError(t, err)
	assert.Len(t, result.Installed, 1)

	// Verify the constraint preserved "warn" from the existing constraint
	constraint := fakeClient.constraints["test-constraint"]
	require.NotNil(t, constraint)
	action, _, _ := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
	assert.Equal(t, "warn", action)
}

func TestInstallDuplicateBundleAndPositional(t *testing.T) {
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{
			{
				Name:              "policy-a",
				Version:           "v1.0.0",
				TemplatePath:      "templates/policy-a.yaml",
				BundleConstraints: map[string]string{"test-bundle": "constraints/policy-a.yaml"},
			},
		},
		Bundles: []catalog.Bundle{
			{Name: "test-bundle", Policies: []string{"policy-a"}},
		},
	}

	fakeClient := NewFakeClient()
	fetcher := &FakeFetcher{
		content: map[string][]byte{
			"templates/policy-a.yaml": []byte(`apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: policy-a
`),
			"constraints/policy-a.yaml": []byte(`apiVersion: constraints.gatekeeper.sh/v1beta1
kind: PolicyA
metadata:
  name: policy-a-constraint
`),
		},
	}

	// Specify same policy via both bundle AND positional — should deduplicate
	opts := &InstallOptions{
		Policies: []string{"policy-a"},
		Bundles:  []string{"test-bundle"},
	}

	result, err := Install(context.Background(), fakeClient, fetcher, cat, opts)
	require.NoError(t, err)
	assert.Len(t, result.Installed, 1)
	assert.Equal(t, 1, result.TemplatesInstalled)
}

// --- #25: uninstall.go unit tests ---

func TestUninstallDryRun(t *testing.T) {
	fakeClient := NewFakeClient()
	tmpl := &unstructured.Unstructured{}
	tmpl.SetName("test-policy")
	tmpl.SetLabels(map[string]string{
		labels.LabelManagedBy: labels.ManagedByValue,
	})
	tmpl.SetAnnotations(map[string]string{
		labels.AnnotationSource: catalog.DefaultRepository,
	})
	fakeClient.templates["test-policy"] = tmpl

	opts := UninstallOptions{
		Policies: []string{"test-policy"},
		DryRun:   true,
	}

	result, err := Uninstall(context.Background(), fakeClient, opts)
	require.NoError(t, err)
	assert.Len(t, result.Uninstalled, 1)

	// Verify template was NOT actually deleted
	_, err = fakeClient.GetTemplate(context.Background(), "test-policy")
	assert.NoError(t, err)
}

func TestUninstallGatekeeperNotInstalled(t *testing.T) {
	fakeClient := NewFakeClient()
	fakeClient.gatekeeperInstalled = false

	opts := UninstallOptions{
		Policies: []string{"test-policy"},
	}

	_, err := Uninstall(context.Background(), fakeClient, opts)
	require.Error(t, err)
	var notInstalledErr *GatekeeperNotInstalledError
	assert.ErrorAs(t, err, &notInstalledErr)
}

func TestUninstallMultiplePolicies(t *testing.T) {
	fakeClient := NewFakeClient()
	for _, name := range []string{"policy1", "policy2", "policy3"} {
		tmpl := &unstructured.Unstructured{}
		tmpl.SetName(name)
		tmpl.SetLabels(map[string]string{
			labels.LabelManagedBy: labels.ManagedByValue,
		})
		tmpl.SetAnnotations(map[string]string{
			labels.AnnotationSource: catalog.DefaultRepository,
		})
		fakeClient.templates[name] = tmpl
	}

	opts := UninstallOptions{
		Policies: []string{"policy1", "policy2", "policy3"},
	}

	result, err := Uninstall(context.Background(), fakeClient, opts)
	require.NoError(t, err)
	assert.Len(t, result.Uninstalled, 3)
	assert.Empty(t, result.Failed)
}

func TestUninstallMixedResults(t *testing.T) {
	fakeClient := NewFakeClient()
	// One managed policy
	tmpl := &unstructured.Unstructured{}
	tmpl.SetName("managed-policy")
	tmpl.SetLabels(map[string]string{
		labels.LabelManagedBy: labels.ManagedByValue,
	})
	tmpl.SetAnnotations(map[string]string{
		labels.AnnotationSource: catalog.DefaultRepository,
	})
	fakeClient.templates["managed-policy"] = tmpl

	opts := UninstallOptions{
		Policies: []string{"managed-policy", "nonexistent-policy"},
	}

	result, err := Uninstall(context.Background(), fakeClient, opts)
	require.NoError(t, err)
	assert.Len(t, result.Uninstalled, 1)
	assert.Equal(t, "managed-policy", result.Uninstalled[0])
	assert.Len(t, result.NotFound, 1)
	assert.Equal(t, "nonexistent-policy", result.NotFound[0])
}

// --- #30: client.go coverage improvements ---

func TestK8sClient_InstallConstraint(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8sClient := &K8sClient{dynamicClient: fakeClient}

	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": "require-labels",
			},
		},
	}

	// Install new constraint (create path)
	err := k8sClient.InstallConstraint(context.Background(), constraint)
	require.NoError(t, err)

	// Verify it was created
	gvr := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}
	result, err := fakeClient.Resource(gvr).Get(context.Background(), "require-labels", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "require-labels", result.GetName())
}

func TestK8sClient_GetConstraint(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8sClient := &K8sClient{dynamicClient: fakeClient}

	gvr := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	// Pre-create constraint using the correct GVR
	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": "require-labels",
			},
		},
	}
	_, err := fakeClient.Resource(gvr).Create(context.Background(), constraint, metav1.CreateOptions{})
	require.NoError(t, err)

	// Get existing constraint
	result, err := k8sClient.GetConstraint(context.Background(), gvr, "require-labels")
	require.NoError(t, err)
	assert.Equal(t, "require-labels", result.GetName())

	// Get non-existent constraint
	_, err = k8sClient.GetConstraint(context.Background(), gvr, "nonexistent")
	assert.Error(t, err)
}

func TestK8sClient_DeleteConstraint(t *testing.T) {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClient(scheme)
	k8sClient := &K8sClient{dynamicClient: fakeClient}

	gvr := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: "k8srequiredlabels",
	}

	// Pre-create constraint
	constraint := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "K8sRequiredLabels",
			"metadata": map[string]interface{}{
				"name": "require-labels",
			},
		},
	}
	_, err := fakeClient.Resource(gvr).Create(context.Background(), constraint, metav1.CreateOptions{})
	require.NoError(t, err)

	// Delete existing constraint
	err = k8sClient.DeleteConstraint(context.Background(), gvr, "require-labels")
	require.NoError(t, err)

	// Delete non-existent should not error
	err = k8sClient.DeleteConstraint(context.Background(), gvr, "nonexistent")
	require.NoError(t, err)
}

func TestIsCRDNotRegisteredError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "nil error", err: nil, expected: false},
		{name: "no matches error", err: fmt.Errorf("no matches for kind \"ConstraintTemplate\""), expected: true},
		{name: "resource not found server error", err: fmt.Errorf("the server could not find the requested resource"), expected: true},
		{name: "other error", err: fmt.Errorf("connection refused"), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCRDNotRegisteredError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConstraintGVR(t *testing.T) {
	gvr := constraintGVR("K8sRequiredLabels")
	assert.Equal(t, "constraints.gatekeeper.sh", gvr.Group)
	assert.Equal(t, "v1beta1", gvr.Version)
	assert.Equal(t, "k8srequiredlabels", gvr.Resource)
}

func TestSetEnforcementAction(t *testing.T) {
	t.Run("set action on existing spec", func(t *testing.T) {
		constraint := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "constraints.gatekeeper.sh/v1beta1",
				"kind":       "TestPolicy",
				"metadata": map[string]interface{}{
					"name": "test",
				},
				"spec": map[string]interface{}{
					"enforcementAction": "deny",
				},
			},
		}

		err := setEnforcementAction(constraint, "warn")
		require.NoError(t, err)

		action, _, _ := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
		assert.Equal(t, "warn", action)
	})

	t.Run("set action on missing spec", func(t *testing.T) {
		constraint := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "constraints.gatekeeper.sh/v1beta1",
				"kind":       "TestPolicy",
				"metadata": map[string]interface{}{
					"name": "test",
				},
			},
		}

		err := setEnforcementAction(constraint, "dryrun")
		require.NoError(t, err)

		action, _, _ := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
		assert.Equal(t, "dryrun", action)
	})
}
