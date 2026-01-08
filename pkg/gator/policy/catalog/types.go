package catalog

import (
	"fmt"
	"sync"
	"time"
)

// PolicyCatalog represents the root catalog structure for policy discovery.
type PolicyCatalog struct {
	APIVersion string          `json:"apiVersion" yaml:"apiVersion"`
	Kind       string          `json:"kind" yaml:"kind"`
	Metadata   CatalogMetadata `json:"metadata" yaml:"metadata"`
	Bundles    []Bundle        `json:"bundles" yaml:"bundles"`
	Policies   []Policy        `json:"policies" yaml:"policies"`

	// Cached indexes for O(1) lookups (built lazily, thread-safe)
	policyIndex     map[string]int `json:"-" yaml:"-"`
	bundleIndex     map[string]int `json:"-" yaml:"-"`
	policyIndexOnce sync.Once     `json:"-" yaml:"-"`
	bundleIndexOnce sync.Once     `json:"-" yaml:"-"`
}

// CatalogMetadata contains metadata about the catalog itself.
type CatalogMetadata struct {
	Name       string    `json:"name" yaml:"name"`
	Version    string    `json:"version" yaml:"version"`
	UpdatedAt  time.Time `json:"updatedAt" yaml:"updatedAt"`
	Repository string    `json:"repository" yaml:"repository"`
}

// Bundle represents a curated set of policies with pre-configured constraints.
type Bundle struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Inherits    string   `json:"inherits,omitempty" yaml:"inherits,omitempty"`
	Policies    []string `json:"policies" yaml:"policies"`
}

// Policy represents a single policy available in the catalog.
type Policy struct {
	Name                 string   `json:"name" yaml:"name"`
	Version              string   `json:"version" yaml:"version"`
	Description          string   `json:"description" yaml:"description"`
	Category             string   `json:"category" yaml:"category"`
	TemplatePath         string   `json:"templatePath" yaml:"templatePath"`
	ConstraintPath       string   `json:"constraintPath,omitempty" yaml:"constraintPath,omitempty"`
	SampleConstraintPath string   `json:"sampleConstraintPath,omitempty" yaml:"sampleConstraintPath,omitempty"`
	DocumentationURL     string   `json:"documentationUrl,omitempty" yaml:"documentationUrl,omitempty"`
	Bundles              []string `json:"bundles,omitempty" yaml:"bundles,omitempty"`
}

// GetPolicy returns the policy with the given name, or nil if not found.
// Uses O(1) indexed lookup after first call. Thread-safe.
func (c *PolicyCatalog) GetPolicy(name string) *Policy {
	// Build index lazily on first lookup (thread-safe)
	c.policyIndexOnce.Do(func() {
		c.policyIndex = make(map[string]int, len(c.Policies))
		for i := range c.Policies {
			c.policyIndex[c.Policies[i].Name] = i
		}
	})
	if idx, ok := c.policyIndex[name]; ok {
		return &c.Policies[idx]
	}
	return nil
}

// GetBundle returns the bundle with the given name, or nil if not found.
// Uses O(1) indexed lookup after first call. Thread-safe.
func (c *PolicyCatalog) GetBundle(name string) *Bundle {
	// Build index lazily on first lookup (thread-safe)
	c.bundleIndexOnce.Do(func() {
		c.bundleIndex = make(map[string]int, len(c.Bundles))
		for i := range c.Bundles {
			c.bundleIndex[c.Bundles[i].Name] = i
		}
	})
	if idx, ok := c.bundleIndex[name]; ok {
		return &c.Bundles[idx]
	}
	return nil
}

// MaxInheritanceDepth is the maximum depth of bundle inheritance to prevent runaway expansion.
const MaxInheritanceDepth = 10

// ResolveBundlePolicies returns all policy names for a bundle, including inherited policies.
// Inheritance is processed parent-first (deepest ancestor first), so child policies can override.
func (c *PolicyCatalog) ResolveBundlePolicies(bundleName string) ([]string, error) {
	bundle := c.GetBundle(bundleName)
	if bundle == nil {
		return nil, &BundleNotFoundError{Name: bundleName}
	}

	// First, build the inheritance chain from child to ancestors
	var inheritanceChain []*Bundle
	visitedBundles := make(map[string]bool)
	current := bundle

	for current != nil {
		// Check for circular inheritance
		if visitedBundles[current.Name] {
			return nil, fmt.Errorf("circular inheritance detected in bundle: %s", current.Name)
		}
		visitedBundles[current.Name] = true

		// Check depth limit
		if len(inheritanceChain) >= MaxInheritanceDepth {
			return nil, fmt.Errorf("bundle inheritance exceeds maximum depth of %d", MaxInheritanceDepth)
		}

		inheritanceChain = append(inheritanceChain, current)

		if current.Inherits == "" {
			break
		}
		parent := c.GetBundle(current.Inherits)
		if parent == nil {
			return nil, &BundleNotFoundError{Name: current.Inherits}
		}
		current = parent
	}

	// Now process in reverse order (parent-first, deepest ancestor first)
	// This ensures parent policies are added first, and child policies can be deduplicated
	seen := make(map[string]bool)
	var policies []string

	for i := len(inheritanceChain) - 1; i >= 0; i-- {
		b := inheritanceChain[i]
		for _, p := range b.Policies {
			if !seen[p] {
				seen[p] = true
				policies = append(policies, p)
			}
		}
	}

	return policies, nil
}

// BundleNotFoundError is returned when a bundle cannot be found.
type BundleNotFoundError struct {
	Name string
}

func (e *BundleNotFoundError) Error() string {
	return "bundle not found: " + e.Name
}

// PolicyNotFoundError is returned when a policy cannot be found.
type PolicyNotFoundError struct {
	Name string
}

func (e *PolicyNotFoundError) Error() string {
	return "policy not found: " + e.Name
}
