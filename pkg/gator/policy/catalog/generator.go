package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	semver "github.com/blang/semver/v4"
	"sigs.k8s.io/yaml"
)

// GeneratorOptions contains options for generating a catalog.
type GeneratorOptions struct {
	// LibraryPath is the root path of the gatekeeper-library repository.
	LibraryPath string
	// CatalogName is the name of the catalog.
	CatalogName string
	// CatalogVersion is the version of the catalog.
	CatalogVersion string
	// Repository is the repository URL.
	Repository string
	// BundlesFile is an optional path to a bundles definition file.
	BundlesFile string
	// BaseURL is the base URL for template/constraint paths.
	// If set, paths will be converted to full URLs (e.g., https://raw.githubusercontent.com/.../library/...).
	// If empty, relative paths will be used (e.g., library/...).
	BaseURL string
}

// Bundle annotation key for metadata.gatekeeper.sh/bundle.
const bundleAnnotationKey = "metadata.gatekeeper.sh/bundle"

// bundleDescriptions provides descriptions for well-known bundles.
var bundleDescriptions = map[string]string{
	"pod-security-baseline": `Enforces Pod Security Standards at Baseline level. Prevents known privilege escalations.
See https://kubernetes.io/docs/concepts/security/pod-security-standards/`,
	"pod-security-restricted": `Enforces Pod Security Standards at Restricted level. Includes all Baseline controls plus additional hardening.
See https://kubernetes.io/docs/concepts/security/pod-security-standards/`,
}

// GenerateCatalog generates a PolicyCatalog from a gatekeeper-library directory structure.
func GenerateCatalog(opts *GeneratorOptions) (*PolicyCatalog, error) {
	libraryPath := filepath.Join(opts.LibraryPath, "library")
	if _, err := os.Stat(libraryPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("library directory not found at %s", libraryPath)
	}

	catalog := &PolicyCatalog{
		APIVersion: "gator.gatekeeper.sh/v1alpha1",
		Kind:       "PolicyCatalog",
		Metadata: CatalogMetadata{
			Name:       opts.CatalogName,
			Version:    opts.CatalogVersion,
			UpdatedAt:  time.Now().UTC(),
			Repository: opts.Repository,
		},
		Bundles:  []Bundle{},
		Policies: []Policy{},
	}

	// Walk the library directory to find policies
	err := filepath.Walk(libraryPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Look for template.yaml files
		if info.IsDir() || info.Name() != "template.yaml" {
			return nil
		}

		policy, err := parsePolicyFromTemplate(path, opts.LibraryPath)
		if err != nil {
			return fmt.Errorf("parsing template at %s: %w", path, err)
		}

		if policy != nil {
			catalog.Policies = append(catalog.Policies, *policy)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking library directory: %w", err)
	}

	// Sort policies by name for consistent output
	sort.Slice(catalog.Policies, func(i, j int) bool {
		return catalog.Policies[i].Name < catalog.Policies[j].Name
	})

	// Load bundles from bundles file if provided
	if opts.BundlesFile != "" {
		bundles, err := loadBundlesFile(opts.BundlesFile)
		if err != nil {
			return nil, fmt.Errorf("loading bundles file: %w", err)
		}
		catalog.Bundles = bundles

		// Update policies with bundle membership
		updatePolicyBundles(catalog)
	} else {
		// Auto-generate Pod Security Standards bundles
		generatePSSBundles(catalog)
	}

	// Convert paths to URLs if base URL is provided
	if opts.BaseURL != "" {
		convertPathsToURLs(catalog, opts.BaseURL)
	}

	return catalog, nil
}

// parsePolicyFromTemplate reads a template.yaml and extracts policy metadata.
func parsePolicyFromTemplate(templatePath, libraryRoot string) (*Policy, error) {
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("reading template file: %w", err)
	}

	// Parse template to extract metadata
	var template struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name        string            `yaml:"name"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"metadata"`
	}

	if err := yaml.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("parsing template YAML: %w", err)
	}

	if template.Kind != "ConstraintTemplate" {
		return nil, nil // Not a constraint template
	}

	// Get relative path from library root
	relPath, err := filepath.Rel(libraryRoot, templatePath)
	if err != nil {
		return nil, fmt.Errorf("getting relative path: %w", err)
	}

	// Determine category from path (e.g., library/general/requiredlabels -> general)
	category := extractCategory(relPath)

	// Extract version from annotations or use default
	// The gatekeeper-library uses metadata.gatekeeper.sh/version annotation
	version := "v1.0.0"
	if v, ok := template.Metadata.Annotations["metadata.gatekeeper.sh/version"]; ok {
		version = normalizeVersion(v)
	}

	// Get description from annotations
	description := ""
	if desc, ok := template.Metadata.Annotations["description"]; ok {
		description = desc
	}

	// Look for constraint files in samples directory
	templateDir := filepath.Dir(templatePath)

	// Get documentation URL
	docURL := ""
	if url, ok := template.Metadata.Annotations["policy.gatekeeper.sh/docs"]; ok {
		docURL = url
	}

	// Get bundle membership from annotation
	var bundles []string
	if bundleStr, ok := template.Metadata.Annotations[bundleAnnotationKey]; ok {
		// Parse comma-separated bundle names
		for _, b := range strings.Split(bundleStr, ",") {
			bundleName := strings.TrimSpace(b)
			if bundleName != "" {
				bundles = append(bundles, bundleName)
			}
		}
	}

	// Get Kubernetes version compatibility from annotations
	minK8sVersion := normalizeVersion(template.Metadata.Annotations["metadata.gatekeeper.sh/minKubernetesVersion"])
	maxK8sVersion := normalizeVersion(template.Metadata.Annotations["metadata.gatekeeper.sh/maxKubernetesVersion"])

	// When an explicit annotation is absent, fall back to deriving the bound
	// from the API lifecycle of the built-in resources the policy targets.
	// Explicit annotations always win, and each bound is derived independently.
	if minK8sVersion == "" || maxK8sVersion == "" {
		derivedMin, derivedMax := deriveK8sVersionRange(targetGroupKinds(templateDir))
		if minK8sVersion == "" {
			minK8sVersion = derivedMin
		}
		if maxK8sVersion == "" {
			maxK8sVersion = derivedMax
		}
	}

	// Build BundleConstraints by discovering per-bundle constraint files.
	// The library convention is that sample directories with names containing
	// a bundle keyword (e.g., "baseline", "restricted") provide bundle-specific
	// constraint configurations. If no bundle-specific directory is found,
	// the first constraint file discovered is used as a fallback.
	bundleConstraints := findBundleConstraints(templateDir, libraryRoot, bundles)

	policy := &Policy{
		Name:                 template.Metadata.Name,
		Version:              version,
		Description:          description,
		Category:             category,
		TemplatePath:         relPath,
		BundleConstraints:    bundleConstraints,
		DocumentationURL:     docURL,
		Bundles:              bundles,
		MinKubernetesVersion: minK8sVersion,
		MaxKubernetesVersion: maxK8sVersion,
	}

	return policy, nil
}

// extractCategory extracts the category from the library path.
func extractCategory(relPath string) string {
	// Path format: library/<category>/<policy>/template.yaml
	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) >= 2 && parts[0] == "library" {
		category := parts[1]
		// Normalize category names
		category = strings.ReplaceAll(category, "-", " ")
		category = strings.ReplaceAll(category, "_", " ")
		// Handle pod-security-policy -> pod-security
		if category == "pod security policy" {
			return "pod-security"
		}
		return strings.ReplaceAll(category, " ", "-")
	}
	return "general"
}

// findBundleConstraints discovers per-bundle constraint files in the samples directory.
// It maps each bundle to its constraint file by matching sample directory names
// to bundle keywords. For example, bundle "pod-security-baseline" matches a sample
// directory named "psp-capabilities-baseline" (contains "baseline").
// If no bundle-specific match is found, the first constraint file is used as fallback.
// Returns nil if no bundles or no constraint files are found.
func findBundleConstraints(templateDir, libraryRoot string, bundles []string) map[string]string {
	if len(bundles) == 0 {
		return nil
	}

	samplesDir := filepath.Join(templateDir, "samples")
	if _, err := os.Stat(samplesDir); os.IsNotExist(err) {
		return nil
	}

	// Scan all sample subdirectories for constraint files, indexed by dir name.
	// constraintsByDir maps sample directory name → relative constraint path.
	constraintsByDir := make(map[string]string)
	var fallbackPath string

	entries, err := os.ReadDir(samplesDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(samplesDir, entry.Name())
		constraintFile := findConstraintFile(dirPath)
		if constraintFile == "" {
			continue
		}

		data, readErr := os.ReadFile(constraintFile)
		if readErr != nil {
			continue
		}
		if !isConstraintFile(data) {
			continue
		}

		relPath, relErr := filepath.Rel(libraryRoot, constraintFile)
		if relErr != nil {
			continue
		}

		constraintsByDir[entry.Name()] = relPath
		if fallbackPath == "" {
			fallbackPath = relPath
		}
	}

	if len(constraintsByDir) == 0 {
		return nil
	}

	// Match bundles to sample directories.
	// Extract the distinguishing keyword from bundle name (last segment after "pod-security-").
	// e.g., "pod-security-baseline" → "baseline", "pod-security-restricted" → "restricted"
	result := make(map[string]string, len(bundles))
	for _, bundle := range bundles {
		// Extract keyword: use the last hyphen-separated segment of the bundle name
		keyword := bundle
		if idx := strings.LastIndex(bundle, "-"); idx >= 0 {
			keyword = bundle[idx+1:]
		}

		// Look for a sample dir whose name contains the keyword
		matched := false
		for dirName, cPath := range constraintsByDir {
			if strings.Contains(strings.ToLower(dirName), strings.ToLower(keyword)) {
				result[bundle] = cPath
				matched = true
				break
			}
		}
		if !matched {
			result[bundle] = fallbackPath
		}
	}

	return result
}

// constraintFilenames are the file names a sample constraint may use.
var constraintFilenames = []string{"constraint.yaml", "constraint.yml"}

// findConstraintFile returns the path to the constraint file in dir, or "" if
// none exists.
func findConstraintFile(dir string) string {
	for _, name := range constraintFilenames {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// isConstraintFile checks if the YAML content is a Constraint.
func isConstraintFile(data []byte) bool {
	var doc struct {
		APIVersion string `yaml:"apiVersion"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return strings.HasPrefix(doc.APIVersion, "constraints.gatekeeper.sh/")
}

// BundlesFile represents the structure of a bundles definition file.
type BundlesFile struct {
	Bundles []Bundle `yaml:"bundles" json:"bundles"`
}

// loadBundlesFile loads bundle definitions from a YAML file.
func loadBundlesFile(path string) ([]Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading bundles file: %w", err)
	}

	var bundlesFile BundlesFile
	if err := yaml.Unmarshal(data, &bundlesFile); err != nil {
		return nil, fmt.Errorf("parsing bundles YAML: %w", err)
	}

	return bundlesFile.Bundles, nil
}

// updatePolicyBundles updates the Bundles field of each policy based on bundle membership.
func updatePolicyBundles(catalog *PolicyCatalog) {
	policyBundles := make(map[string][]string)

	// Build reverse mapping from policy to bundles
	for _, bundle := range catalog.Bundles {
		for _, policyName := range bundle.Policies {
			policyBundles[policyName] = append(policyBundles[policyName], bundle.Name)
		}
	}

	// Update policies
	for i := range catalog.Policies {
		if bundles, ok := policyBundles[catalog.Policies[i].Name]; ok {
			catalog.Policies[i].Bundles = bundles
		}
	}
}

// WriteCatalog writes a PolicyCatalog to a YAML file.
func WriteCatalog(catalog *PolicyCatalog, outputPath string) error {
	data, err := yaml.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("marshaling catalog: %w", err)
	}

	// Add header comment
	header := `# Policy Catalog for Gatekeeper Library
#
# This file is auto-generated. Do not edit directly.
# Generated at: ` + time.Now().UTC().Format(time.RFC3339) + `
#
# To regenerate, run:
#   gator policy generate-catalog --library-path=. --output=catalog.yaml
#
`
	output := header + string(data)

	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		return fmt.Errorf("writing catalog file: %w", err)
	}

	return nil
}

// ValidateCatalogSchema validates a catalog against the expected schema.
func ValidateCatalogSchema(catalog *PolicyCatalog) error {
	if catalog.APIVersion == "" {
		return fmt.Errorf("missing apiVersion")
	}
	if catalog.Kind != "PolicyCatalog" {
		return fmt.Errorf("invalid kind: expected PolicyCatalog, got %s", catalog.Kind)
	}
	if catalog.Metadata.Name == "" {
		return fmt.Errorf("missing metadata.name")
	}
	if catalog.Metadata.Version == "" {
		return fmt.Errorf("missing metadata.version")
	}

	// Validate policies
	policyNames := make(map[string]bool)
	for i := range catalog.Policies {
		policy := &catalog.Policies[i]
		if policy.Name == "" {
			return fmt.Errorf("policy missing name")
		}
		if policyNames[policy.Name] {
			return fmt.Errorf("duplicate policy name: %s", policy.Name)
		}
		policyNames[policy.Name] = true

		if policy.TemplatePath == "" {
			return fmt.Errorf("policy %s missing templatePath", policy.Name)
		}
		if !isValidVersion(policy.Version) {
			return fmt.Errorf("policy %s has invalid version: %s", policy.Name, policy.Version)
		}
		var minV, maxV semver.Version
		if policy.MinKubernetesVersion != "" {
			v, err := parseVersion(policy.MinKubernetesVersion)
			if err != nil {
				return fmt.Errorf("policy %s has invalid minKubernetesVersion: %s", policy.Name, policy.MinKubernetesVersion)
			}
			minV = v
		}
		if policy.MaxKubernetesVersion != "" {
			v, err := parseVersion(policy.MaxKubernetesVersion)
			if err != nil {
				return fmt.Errorf("policy %s has invalid maxKubernetesVersion: %s", policy.Name, policy.MaxKubernetesVersion)
			}
			maxV = v
		}
		if policy.MinKubernetesVersion != "" && policy.MaxKubernetesVersion != "" && minV.Compare(maxV) > 0 {
			return fmt.Errorf("policy %s has minKubernetesVersion (%s) greater than maxKubernetesVersion (%s)",
				policy.Name, policy.MinKubernetesVersion, policy.MaxKubernetesVersion)
		}
	}

	// Validate bundles reference existing policies
	for _, bundle := range catalog.Bundles {
		if bundle.Name == "" {
			return fmt.Errorf("bundle missing name")
		}
		for _, policyName := range bundle.Policies {
			if !policyNames[policyName] {
				return fmt.Errorf("bundle %s references non-existent policy: %s", bundle.Name, policyName)
			}
		}
	}

	return nil
}

func normalizeVersion(v string) string {
	if v != "" && !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// parseVersion parses a version string leniently: it tolerates a leading "v",
// two-component "major.minor" values (the conventional form for Kubernetes
// versions, e.g. "1.30"), and leading zeros, padding a missing patch to 0.
func parseVersion(v string) (semver.Version, error) {
	return semver.ParseTolerant(v)
}

// isValidVersion checks if a version string is valid semver format.
func isValidVersion(version string) bool {
	if version == "" {
		return false
	}
	_, err := parseVersion(version)
	return err == nil
}

// coreVersion strips any pre-release and build metadata, leaving only the
// major.minor.patch components. Kubernetes server versions reported by distros
// carry vendor suffixes (e.g. "v1.30.2-gke.1234", "v1.28.3+k3s1") that must not
// affect compatibility comparisons.
func coreVersion(v semver.Version) semver.Version {
	return semver.Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch}
}

// K8sVersionInRange reports whether serverVersion falls within the inclusive
// [minVersion, maxVersion] range. An empty minVersion or maxVersion means the
// range is unbounded on that side, so a policy with neither bound is compatible
// with every cluster. Only the core major.minor.patch of each version is
// compared; vendor pre-release/build suffixes are ignored. It returns an error
// if any non-empty version string cannot be parsed.
func K8sVersionInRange(serverVersion, minVersion, maxVersion string) (bool, error) {
	sv, err := parseVersion(serverVersion)
	if err != nil {
		return false, fmt.Errorf("parsing server version %q: %w", serverVersion, err)
	}
	sv = coreVersion(sv)

	if minVersion != "" {
		mv, err := parseVersion(minVersion)
		if err != nil {
			return false, fmt.Errorf("parsing minKubernetesVersion %q: %w", minVersion, err)
		}
		if sv.Compare(coreVersion(mv)) < 0 {
			return false, nil
		}
	}

	if maxVersion != "" {
		mv, err := parseVersion(maxVersion)
		if err != nil {
			return false, fmt.Errorf("parsing maxKubernetesVersion %q: %w", maxVersion, err)
		}
		if sv.Compare(coreVersion(mv)) > 0 {
			return false, nil
		}
	}

	return true, nil
}

// FormatK8sVersionRange renders a min/max Kubernetes version constraint as a
// human-readable range string ("min - max", ">=min", "<=max"), returning "-"
// when neither bound is set.
func FormatK8sVersionRange(minVersion, maxVersion string) string {
	switch {
	case minVersion != "" && maxVersion != "":
		return minVersion + " - " + maxVersion
	case minVersion != "":
		return ">=" + minVersion
	case maxVersion != "":
		return "<=" + maxVersion
	default:
		return "-"
	}
}

// generatePSSBundles auto-generates bundles from policy annotations.
// Bundles are created based on metadata.gatekeeper.sh/bundle annotations in templates.
func generatePSSBundles(catalog *PolicyCatalog) {
	// Build mapping from bundle name to policies
	bundlePolicies := make(map[string][]string)

	for i := range catalog.Policies {
		policy := &catalog.Policies[i]
		for _, bundleName := range policy.Bundles {
			bundlePolicies[bundleName] = append(bundlePolicies[bundleName], policy.Name)
		}
	}

	// Create bundles from the mapping
	// Sort bundle names for consistent output
	var bundleNames []string
	for name := range bundlePolicies {
		bundleNames = append(bundleNames, name)
	}
	sort.Strings(bundleNames)

	for _, name := range bundleNames {
		policies := bundlePolicies[name]
		sort.Strings(policies) // Sort policy names for consistent output

		description := fmt.Sprintf("Policy bundle: %s", name)
		if desc, ok := bundleDescriptions[name]; ok {
			description = desc
		}

		bundle := Bundle{
			Name:        name,
			Description: description,
			Policies:    policies,
		}
		catalog.Bundles = append(catalog.Bundles, bundle)
	}
}

// convertPathsToURLs converts all relative paths in the catalog to full URLs.
// The baseURL should be the raw content URL prefix (e.g., https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master).
func convertPathsToURLs(catalog *PolicyCatalog, baseURL string) {
	// Ensure baseURL doesn't have trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	for i := range catalog.Policies {
		policy := &catalog.Policies[i]

		if policy.TemplatePath != "" && !strings.HasPrefix(policy.TemplatePath, "http") {
			policy.TemplatePath = baseURL + "/" + policy.TemplatePath
		}
		for bundle, cPath := range policy.BundleConstraints {
			if cPath != "" && !strings.HasPrefix(cPath, "http") {
				policy.BundleConstraints[bundle] = baseURL + "/" + cPath
			}
		}
	}
}
