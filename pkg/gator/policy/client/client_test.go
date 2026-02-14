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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
