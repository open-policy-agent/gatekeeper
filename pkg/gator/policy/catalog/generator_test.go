package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCatalog(t *testing.T) {
	// Create a temporary test library structure
	tempDir := t.TempDir()

	// Create library structure
	libraryDir := filepath.Join(tempDir, "library")
	pspDir := filepath.Join(libraryDir, "pod-security-policy")
	testPolicyDir := filepath.Join(pspDir, "test-policy")
	err := os.MkdirAll(testPolicyDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create a template.yaml file
	templateContent := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8stestpolicy
  annotations:
    description: "A test policy for unit testing"
    metadata.gatekeeper.sh/version: "1.0.0"
spec:
  crd:
    spec:
      names:
        kind: K8sTestPolicy
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8stestpolicy
        violation[{"msg": msg}] {
          msg := "test"
        }
`
	templatePath := filepath.Join(testPolicyDir, "template.yaml")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0o600); err != nil {
		t.Fatalf("Failed to write template.yaml: %v", err)
	}

	// Create a samples directory with constraint
	samplesDir := filepath.Join(testPolicyDir, "samples")
	if err := os.MkdirAll(samplesDir, 0o755); err != nil {
		t.Fatalf("Failed to create samples directory: %v", err)
	}

	constraintContent := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTestPolicy
metadata:
  name: test-policy-example
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`
	constraintPath := filepath.Join(samplesDir, "constraint.yaml")
	if err := os.WriteFile(constraintPath, []byte(constraintContent), 0o600); err != nil {
		t.Fatalf("Failed to write constraint.yaml: %v", err)
	}

	// Generate catalog
	opts := &GeneratorOptions{
		LibraryPath:    tempDir,
		CatalogName:    "test-catalog",
		CatalogVersion: "v1.0.0",
		Repository:     "https://example.com/test-catalog",
	}

	catalog, err := GenerateCatalog(opts)
	if err != nil {
		t.Fatalf("GenerateCatalog failed: %v", err)
	}

	// Verify catalog contents
	if catalog.Metadata.Name != "test-catalog" {
		t.Errorf("Expected catalog name 'test-catalog', got '%s'", catalog.Metadata.Name)
	}

	if catalog.Metadata.Version != "v1.0.0" {
		t.Errorf("Expected catalog version 'v1.0.0', got '%s'", catalog.Metadata.Version)
	}

	if catalog.Metadata.Repository != "https://example.com/test-catalog" {
		t.Errorf("Expected repository 'https://example.com/test-catalog', got '%s'", catalog.Metadata.Repository)
	}

	// Check that at least one policy was found
	if len(catalog.Policies) == 0 {
		t.Error("Expected at least one policy in catalog")
	}

	// Verify the policy details
	found := false
	for _, policy := range catalog.Policies {
		if policy.Name == "k8stestpolicy" {
			found = true
			if policy.Description != "A test policy for unit testing" {
				t.Errorf("Expected description 'A test policy for unit testing', got '%s'", policy.Description)
			}
			if policy.Category != "pod-security" {
				t.Errorf("Expected category 'pod-security', got '%s'", policy.Category)
			}
			if policy.Version != "v1.0.0" {
				t.Errorf("Expected version 'v1.0.0', got '%s'", policy.Version)
			}
		}
	}

	if !found {
		t.Error("Expected to find policy 'k8stestpolicy' in catalog")
	}
}

func TestGenerateCatalogWithBundles(t *testing.T) {
	// Create a temporary test library structure
	tempDir := t.TempDir()

	// Create library structure
	libraryDir := filepath.Join(tempDir, "library")
	generalDir := filepath.Join(libraryDir, "general")
	testPolicyDir := filepath.Join(generalDir, "test-bundle-policy")
	err := os.MkdirAll(testPolicyDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create a template.yaml file
	templateContent := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8stestbundlepolicy
  annotations:
    description: "A test policy for bundle testing"
    metadata.gatekeeper.sh/version: "1.0.0"
spec:
  crd:
    spec:
      names:
        kind: K8sTestBundlePolicy
`
	templatePath := filepath.Join(testPolicyDir, "template.yaml")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0o600); err != nil {
		t.Fatalf("Failed to write template.yaml: %v", err)
	}

	// Create a bundles.yaml file
	bundlesContent := `bundles:
  - name: security-essentials
    description: Essential security policies
    policies:
      - k8stestbundlepolicy
  - name: compliance
    description: Compliance policies
    policies:
      - k8stestbundlepolicy
`
	bundlesPath := filepath.Join(tempDir, "bundles.yaml")
	if err := os.WriteFile(bundlesPath, []byte(bundlesContent), 0o600); err != nil {
		t.Fatalf("Failed to write bundles.yaml: %v", err)
	}

	// Generate catalog
	opts := &GeneratorOptions{
		LibraryPath:    tempDir,
		BundlesFile:    bundlesPath,
		CatalogName:    "test-bundle-catalog",
		CatalogVersion: "v1.0.0",
		Repository:     "https://example.com/test-catalog",
	}

	catalog, err := GenerateCatalog(opts)
	if err != nil {
		t.Fatalf("GenerateCatalog failed: %v", err)
	}

	// Check that bundles were loaded
	if len(catalog.Bundles) != 2 {
		t.Errorf("Expected 2 bundles, got %d", len(catalog.Bundles))
	}

	// Check that policies have bundle assignments
	for _, policy := range catalog.Policies {
		if policy.Name == "k8stestbundlepolicy" {
			if len(policy.Bundles) != 2 {
				t.Errorf("Expected policy to be in 2 bundles, got %d", len(policy.Bundles))
			}
		}
	}
}

func TestWriteCatalog(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "output.yaml")

	catalog := &PolicyCatalog{
		APIVersion: "gator.gatekeeper.sh/v1alpha1",
		Kind:       "PolicyCatalog",
		Metadata: CatalogMetadata{
			Name:       "test",
			Version:    "v1.0.0",
			Repository: "https://example.com",
		},
		Policies: []Policy{
			{
				Name:         "test-policy",
				Version:      "v1.0.0",
				Description:  "A test policy",
				Category:     "general",
				TemplatePath: "library/general/test-policy/template.yaml",
			},
		},
	}

	if err := WriteCatalog(catalog, outputPath); err != nil {
		t.Fatalf("WriteCatalog failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("Output file was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if len(content) == 0 {
		t.Fatal("Output file is empty")
	}

	// Parse back and verify
	parsedCatalog, err := ParseCatalog(content)
	if err != nil {
		t.Fatalf("Failed to parse written catalog: %v", err)
	}

	if parsedCatalog.Metadata.Name != catalog.Metadata.Name {
		t.Errorf("Expected name '%s', got '%s'", catalog.Metadata.Name, parsedCatalog.Metadata.Name)
	}

	if len(parsedCatalog.Policies) != 1 {
		t.Errorf("Expected 1 policy, got %d", len(parsedCatalog.Policies))
	}
}

func TestValidateCatalogSchema(t *testing.T) {
	tests := []struct {
		name        string
		catalog     *PolicyCatalog
		expectError bool
	}{
		{
			name: "valid catalog",
			catalog: &PolicyCatalog{
				APIVersion: "gator.gatekeeper.sh/v1alpha1",
				Kind:       "PolicyCatalog",
				Metadata: CatalogMetadata{
					Name:       "test",
					Version:    "v1.0.0",
					Repository: "https://example.com",
				},
				Policies: []Policy{
					{
						Name:         "test-policy",
						Version:      "v1.0.0",
						Category:     "general",
						TemplatePath: "library/general/test-policy/template.yaml",
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing name",
			catalog: &PolicyCatalog{
				APIVersion: "gator.gatekeeper.sh/v1alpha1",
				Kind:       "PolicyCatalog",
				Metadata: CatalogMetadata{
					Version:    "v1.0.0",
					Repository: "https://example.com",
				},
			},
			expectError: true,
		},
		{
			name: "missing version",
			catalog: &PolicyCatalog{
				APIVersion: "gator.gatekeeper.sh/v1alpha1",
				Kind:       "PolicyCatalog",
				Metadata: CatalogMetadata{
					Name:       "test",
					Repository: "https://example.com",
				},
			},
			expectError: true,
		},
		{
			name: "policy without name",
			catalog: &PolicyCatalog{
				APIVersion: "gator.gatekeeper.sh/v1alpha1",
				Kind:       "PolicyCatalog",
				Metadata: CatalogMetadata{
					Name:       "test",
					Version:    "v1.0.0",
					Repository: "https://example.com",
				},
				Policies: []Policy{
					{
						Version:      "v1.0.0",
						TemplatePath: "library/general/test-policy/template.yaml",
					},
				},
			},
			expectError: true,
		},
		{
			name: "policy without templatePath",
			catalog: &PolicyCatalog{
				APIVersion: "gator.gatekeeper.sh/v1alpha1",
				Kind:       "PolicyCatalog",
				Metadata: CatalogMetadata{
					Name:       "test",
					Version:    "v1.0.0",
					Repository: "https://example.com",
				},
				Policies: []Policy{
					{
						Name:    "test-policy",
						Version: "v1.0.0",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCatalogSchema(tt.catalog)
			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}
		})
	}
}

func TestExtractCategory(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		expected string
	}{
		{
			name:     "pod-security-policy category",
			relPath:  "library/pod-security-policy/my-policy/template.yaml",
			expected: "pod-security",
		},
		{
			name:     "general category",
			relPath:  "library/general/my-policy/template.yaml",
			expected: "general",
		},
		{
			name:     "nested path",
			relPath:  "library/compliance/cis-benchmarks/template.yaml",
			expected: "compliance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCategory(tt.relPath)
			if result != tt.expected {
				t.Errorf("Expected category '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestParsePolicyFromTemplate(t *testing.T) {
	tempDir := t.TempDir()

	// Create library structure
	libraryDir := filepath.Join(tempDir, "library")
	generalDir := filepath.Join(libraryDir, "general")
	policyDir := filepath.Join(generalDir, "requiredlabels")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create a template file
	templateContent := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
  annotations:
    description: "Requires all resources to contain specific labels"
    metadata.gatekeeper.sh/version: "1.2.3"
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          msg := "label missing"
        }
`
	templatePath := filepath.Join(policyDir, "template.yaml")
	if err := os.WriteFile(templatePath, []byte(templateContent), 0o600); err != nil {
		t.Fatalf("Failed to write template: %v", err)
	}

	policy, err := parsePolicyFromTemplate(templatePath, tempDir)
	if err != nil {
		t.Fatalf("parsePolicyFromTemplate failed: %v", err)
	}

	if policy.Name != "k8srequiredlabels" {
		t.Errorf("Expected name 'k8srequiredlabels', got '%s'", policy.Name)
	}

	if policy.Description != "Requires all resources to contain specific labels" {
		t.Errorf("Expected description to match, got '%s'", policy.Description)
	}

	if policy.Version != "v1.2.3" {
		t.Errorf("Expected version 'v1.2.3', got '%s'", policy.Version)
	}

	if policy.Category != "general" {
		t.Errorf("Expected category 'general', got '%s'", policy.Category)
	}
}

func TestConvertPathsToURLs(t *testing.T) {
	catalog := &PolicyCatalog{
		Policies: []Policy{
			{
				Name:                 "test-policy",
				TemplatePath:         "library/general/test/template.yaml",
				ConstraintPath:       "library/general/test/samples/constraint.yaml",
				SampleConstraintPath: "library/general/test/samples/constraint.yaml",
			},
			{
				Name:                 "already-url",
				TemplatePath:         "https://example.com/template.yaml",
				ConstraintPath:       "",
				SampleConstraintPath: "",
			},
		},
	}

	baseURL := "https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master"
	convertPathsToURLs(catalog, baseURL)

	// Check first policy paths are converted
	expectedTemplate := "https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/library/general/test/template.yaml"
	if catalog.Policies[0].TemplatePath != expectedTemplate {
		t.Errorf("Expected templatePath %q, got %q", expectedTemplate, catalog.Policies[0].TemplatePath)
	}

	expectedConstraint := "https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/library/general/test/samples/constraint.yaml"
	if catalog.Policies[0].ConstraintPath != expectedConstraint {
		t.Errorf("Expected constraintPath %q, got %q", expectedConstraint, catalog.Policies[0].ConstraintPath)
	}

	// Check that already-URL paths are not modified
	if catalog.Policies[1].TemplatePath != "https://example.com/template.yaml" {
		t.Errorf("URL path should not be modified, got %q", catalog.Policies[1].TemplatePath)
	}

	// Check that empty paths remain empty
	if catalog.Policies[1].ConstraintPath != "" {
		t.Errorf("Empty path should remain empty, got %q", catalog.Policies[1].ConstraintPath)
	}
}

func TestConvertPathsToURLs_TrailingSlash(t *testing.T) {
	catalog := &PolicyCatalog{
		Policies: []Policy{
			{
				Name:         "test-policy",
				TemplatePath: "library/test/template.yaml",
			},
		},
	}

	// Base URL with trailing slash should work correctly
	baseURL := "https://example.com/repo/"
	convertPathsToURLs(catalog, baseURL)

	expected := "https://example.com/repo/library/test/template.yaml"
	if catalog.Policies[0].TemplatePath != expected {
		t.Errorf("Expected %q, got %q", expected, catalog.Policies[0].TemplatePath)
	}
}
