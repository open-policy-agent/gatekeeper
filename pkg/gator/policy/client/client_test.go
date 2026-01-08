package client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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
					labels.AnnotationSource:      labels.SourceValue,
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
	assert.Contains(t, err.Error(), "kubectl apply")
	assert.Contains(t, err.Error(), "helm install")
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
}

func NewFakeClient() *FakeClient {
	return &FakeClient{
		templates:           make(map[string]*unstructured.Unstructured),
		constraints:         make(map[string]*unstructured.Unstructured),
		gatekeeperInstalled: true,
	}
}

func (c *FakeClient) GatekeeperInstalled(_ context.Context) (bool, error) {
	return c.gatekeeperInstalled, nil
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
	return nil, fmt.Errorf("template not found: %s", name)
}

func (c *FakeClient) InstallTemplate(_ context.Context, template *unstructured.Unstructured) error {
	c.templates[template.GetName()] = template
	return nil
}

func (c *FakeClient) InstallConstraint(_ context.Context, constraint *unstructured.Unstructured) error {
	c.constraints[constraint.GetName()] = constraint
	return nil
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
	return nil
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
			labels.AnnotationSource: labels.SourceValue,
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
		require.NoError(t, err)
		assert.Len(t, result.NotManaged, 1)
		assert.Len(t, result.Failed, 1)
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
			labels.AnnotationSource:  labels.SourceValue,
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
			labels.AnnotationSource:  labels.SourceValue,
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

	count := GetUpgradableCount(installed, cat)
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

	changes := GetUpgradablePolicies(installed, cat)
	assert.Len(t, changes, 1)
	assert.Equal(t, "policy1", changes[0].Name)
	assert.Equal(t, "v1.0.0", changes[0].FromVersion)
	assert.Equal(t, "v2.0.0", changes[0].ToVersion)
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
