package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testVersion = "v1.0.0"

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
		CatalogVersion: testVersion,
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

	if catalog.Metadata.Version != testVersion {
		t.Errorf("Expected catalog version '%s', got '%s'", testVersion, catalog.Metadata.Version)
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
			if policy.Version != testVersion {
				t.Errorf("Expected version '%s', got '%s'", testVersion, policy.Version)
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
				BundleConstraints:    map[string]string{"test-bundle": "library/general/test/samples/constraint.yaml"},
				SampleConstraintPath: "library/general/test/samples/constraint.yaml",
			},
			{
				Name:                 "already-url",
				TemplatePath:         "https://example.com/template.yaml",
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
	if catalog.Policies[0].BundleConstraints["test-bundle"] != expectedConstraint {
		t.Errorf("Expected bundleConstraints[test-bundle] %q, got %q", expectedConstraint, catalog.Policies[0].BundleConstraints["test-bundle"])
	}

	// Check that already-URL paths are not modified
	if catalog.Policies[1].TemplatePath != "https://example.com/template.yaml" {
		t.Errorf("URL path should not be modified, got %q", catalog.Policies[1].TemplatePath)
	}

	// Check that nil BundleConstraints remain nil
	if catalog.Policies[1].BundleConstraints != nil {
		t.Errorf("Nil BundleConstraints should remain nil, got %v", catalog.Policies[1].BundleConstraints)
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

func TestFindConstraintPath_NoSamplesDir(t *testing.T) {
	tempDir := t.TempDir()
	policyDir := filepath.Join(tempDir, "library", "general", "test-policy")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	result := findConstraintPath(policyDir, tempDir)
	if result != "" {
		t.Errorf("Expected empty result when no samples dir, got %q", result)
	}
}

func TestFindConstraintPath_NoConstraintFiles(t *testing.T) {
	tempDir := t.TempDir()
	policyDir := filepath.Join(tempDir, "library", "general", "test-policy")
	samplesDir := filepath.Join(policyDir, "samples")
	if err := os.MkdirAll(samplesDir, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create a non-constraint YAML file
	nonConstraint := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
`
	if err := os.WriteFile(filepath.Join(samplesDir, "configmap.yaml"), []byte(nonConstraint), 0o600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	result := findConstraintPath(policyDir, tempDir)
	if result != "" {
		t.Errorf("Expected empty result when no constraint files, got %q", result)
	}
}

func TestFindConstraintPath_NestedSubdirectories(t *testing.T) {
	tempDir := t.TempDir()
	policyDir := filepath.Join(tempDir, "library", "general", "test-policy")
	nestedDir := filepath.Join(policyDir, "samples", "nested-example")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	constraintContent := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTestPolicy
metadata:
  name: test-example
`
	if err := os.WriteFile(filepath.Join(nestedDir, "constraint.yaml"), []byte(constraintContent), 0o600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	result := findConstraintPath(policyDir, tempDir)
	if result == "" {
		t.Error("Expected to find constraint in nested directory")
	}
	if !strings.Contains(result, "nested-example") {
		t.Errorf("Expected path to contain 'nested-example', got %q", result)
	}
}

func TestFindConstraintPath_YmlExtension(t *testing.T) {
	tempDir := t.TempDir()
	policyDir := filepath.Join(tempDir, "library", "general", "test-policy")
	samplesDir := filepath.Join(policyDir, "samples")
	if err := os.MkdirAll(samplesDir, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	constraintContent := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTestPolicy
metadata:
  name: test-yml
`
	if err := os.WriteFile(filepath.Join(samplesDir, "constraint.yml"), []byte(constraintContent), 0o600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	result := findConstraintPath(policyDir, tempDir)
	if result == "" {
		t.Error("Expected to find constraint with .yml extension")
	}
}

// --- #26: isConstraintFile tests ---

func TestIsConstraintFile(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected bool
	}{
		{
			name:     "valid constraint v1beta1",
			data:     `apiVersion: constraints.gatekeeper.sh/v1beta1`,
			expected: true,
		},
		{
			name:     "non-constraint core API",
			data:     `apiVersion: v1`,
			expected: false,
		},
		{
			name:     "constraint template is not constraint",
			data:     `apiVersion: templates.gatekeeper.sh/v1`,
			expected: false,
		},
		{
			name:     "invalid yaml",
			data:     `{not valid yaml`,
			expected: false,
		},
		{
			name:     "empty content",
			data:     ``,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isConstraintFile([]byte(tt.data))
			if result != tt.expected {
				t.Errorf("isConstraintFile(%q) = %v, want %v", tt.data, result, tt.expected)
			}
		})
	}
}

// --- #26: isValidVersion tests ---

func TestIsValidVersion(t *testing.T) {
	tests := []struct {
		version  string
		expected bool
	}{
		{"v1.0.0", true},
		{"1.0.0", true},
		{"v1.2.3-alpha", true},
		{"v1.2.3+build", true},
		{"v1.2.3-alpha+build", true},
		{"", false},
		{"latest", false},
		{"v1", false},
		{"v1.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			result := isValidVersion(tt.version)
			if result != tt.expected {
				t.Errorf("isValidVersion(%q) = %v, want %v", tt.version, result, tt.expected)
			}
		})
	}
}

// --- #27: generatePSSBundles tests ---

func TestGeneratePSSBundles_EmptyCatalog(t *testing.T) {
	cat := &PolicyCatalog{
		Policies: []Policy{},
		Bundles:  []Bundle{},
	}

	generatePSSBundles(cat)

	if len(cat.Bundles) != 0 {
		t.Errorf("Expected 0 bundles for empty catalog, got %d", len(cat.Bundles))
	}
}

func TestGeneratePSSBundles_WithAnnotations(t *testing.T) {
	cat := &PolicyCatalog{
		Policies: []Policy{
			{
				Name:    "policy1",
				Bundles: []string{"pod-security-baseline"},
			},
			{
				Name:    "policy2",
				Bundles: []string{"pod-security-baseline", "pod-security-restricted"},
			},
			{
				Name:    "policy3",
				Bundles: []string{"pod-security-restricted"},
			},
			{
				Name:    "policy4",
				Bundles: nil, // not in any bundle
			},
		},
		Bundles: []Bundle{},
	}

	generatePSSBundles(cat)

	if len(cat.Bundles) != 2 {
		t.Fatalf("Expected 2 bundles, got %d", len(cat.Bundles))
	}

	// Verify bundles are sorted by name
	if cat.Bundles[0].Name != "pod-security-baseline" {
		t.Errorf("Expected first bundle 'pod-security-baseline', got %q", cat.Bundles[0].Name)
	}
	if cat.Bundles[1].Name != "pod-security-restricted" {
		t.Errorf("Expected second bundle 'pod-security-restricted', got %q", cat.Bundles[1].Name)
	}

	// Verify baseline has 2 policies (policy1, policy2)
	if len(cat.Bundles[0].Policies) != 2 {
		t.Errorf("Expected baseline to have 2 policies, got %d", len(cat.Bundles[0].Policies))
	}

	// Verify restricted has 2 policies (policy2, policy3)
	if len(cat.Bundles[1].Policies) != 2 {
		t.Errorf("Expected restricted to have 2 policies, got %d", len(cat.Bundles[1].Policies))
	}
}

func TestGeneratePSSBundles_KnownDescriptions(t *testing.T) {
	cat := &PolicyCatalog{
		Policies: []Policy{
			{
				Name:    "policy1",
				Bundles: []string{"pod-security-baseline"},
			},
			{
				Name:    "policy2",
				Bundles: []string{"custom-bundle"},
			},
		},
		Bundles: []Bundle{},
	}

	generatePSSBundles(cat)

	// Known bundle should have specific description from bundleDescriptions map
	for _, b := range cat.Bundles {
		if b.Name == "pod-security-baseline" {
			if !strings.Contains(b.Description, "Pod Security Standards") {
				t.Errorf("Expected known description for pod-security-baseline, got %q", b.Description)
			}
		}
		if b.Name == "custom-bundle" {
			// Unknown bundle should have generic description
			if !strings.Contains(b.Description, "Policy bundle:") {
				t.Errorf("Expected generic description for custom-bundle, got %q", b.Description)
			}
		}
	}
}

// --- #27: loadBundlesFile tests ---

func TestLoadBundlesFile(t *testing.T) {
	tempDir := t.TempDir()
	bundlesContent := `bundles:
  - name: test-bundle
    description: Test bundle
    policies:
      - policy1
      - policy2
  - name: other-bundle
    description: Other bundle
    policies:
      - policy3
`
	path := filepath.Join(tempDir, "bundles.yaml")
	if err := os.WriteFile(path, []byte(bundlesContent), 0o600); err != nil {
		t.Fatalf("Failed to write bundles file: %v", err)
	}

	bundles, err := loadBundlesFile(path)
	if err != nil {
		t.Fatalf("loadBundlesFile failed: %v", err)
	}

	if len(bundles) != 2 {
		t.Fatalf("Expected 2 bundles, got %d", len(bundles))
	}
	if bundles[0].Name != "test-bundle" {
		t.Errorf("Expected first bundle 'test-bundle', got %q", bundles[0].Name)
	}
	if len(bundles[0].Policies) != 2 {
		t.Errorf("Expected 2 policies in first bundle, got %d", len(bundles[0].Policies))
	}
}

func TestLoadBundlesFile_NonExistent(t *testing.T) {
	_, err := loadBundlesFile("/nonexistent/bundles.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestLoadBundlesFile_InvalidYAML(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "bundles.yaml")
	if err := os.WriteFile(path, []byte("{invalid yaml: [[["), 0o600); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	_, err := loadBundlesFile(path)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

// --- #27: updatePolicyBundles tests ---

func TestUpdatePolicyBundles(t *testing.T) {
	cat := &PolicyCatalog{
		Policies: []Policy{
			{Name: "policy1"},
			{Name: "policy2"},
			{Name: "policy3"},
		},
		Bundles: []Bundle{
			{Name: "bundle-a", Policies: []string{"policy1", "policy2"}},
			{Name: "bundle-b", Policies: []string{"policy2", "policy3"}},
		},
	}

	updatePolicyBundles(cat)

	// policy1 should be in bundle-a only
	if len(cat.Policies[0].Bundles) != 1 || cat.Policies[0].Bundles[0] != "bundle-a" {
		t.Errorf("Expected policy1 in [bundle-a], got %v", cat.Policies[0].Bundles)
	}

	// policy2 should be in both bundles
	if len(cat.Policies[1].Bundles) != 2 {
		t.Errorf("Expected policy2 in 2 bundles, got %v", cat.Policies[1].Bundles)
	}

	// policy3 should be in bundle-b only
	if len(cat.Policies[2].Bundles) != 1 || cat.Policies[2].Bundles[0] != "bundle-b" {
		t.Errorf("Expected policy3 in [bundle-b], got %v", cat.Policies[2].Bundles)
	}
}

func TestUpdatePolicyBundles_NoBundles(t *testing.T) {
	cat := &PolicyCatalog{
		Policies: []Policy{
			{Name: "policy1"},
		},
		Bundles: []Bundle{},
	}

	updatePolicyBundles(cat)

	// policy1 should have no bundles
	if len(cat.Policies[0].Bundles) != 0 {
		t.Errorf("Expected policy1 in no bundles, got %v", cat.Policies[0].Bundles)
	}
}
