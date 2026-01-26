package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/labels"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

const (
	// DefaultReconcileTimeout is the default timeout for waiting on Gatekeeper to reconcile resources.
	DefaultReconcileTimeout = 120 * time.Second
)

// InstallOptions contains options for installing policies.
type InstallOptions struct {
	// Policies is the list of policy names to install.
	Policies []string
	// Bundle is the bundle name to install.
	Bundle string
	// EnforcementAction overrides the enforcement action for constraints.
	EnforcementAction string
	// DryRun if true, only prints what would be done.
	DryRun bool
}

// InstallResult contains the result of an install operation.
type InstallResult struct {
	// Installed is the list of successfully installed policies.
	Installed []string
	// Skipped is the list of skipped policies (already at same version).
	Skipped []string
	// Failed is the list of policies that failed to install.
	Failed []string
	// Errors contains error messages for failed policies.
	Errors map[string]string
	// ConflictErr is set if a conflict error occurred (resource not managed by gator).
	ConflictErr *ConflictError
	// ConstraintsInstalled is the number of constraints installed.
	ConstraintsInstalled int
	// TemplatesInstalled is the number of templates installed.
	TemplatesInstalled int
	// TotalRequested is the total number of policies requested for installation.
	TotalRequested int
}

// Install installs policies from the catalog.
func Install(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, cat *catalog.PolicyCatalog, opts *InstallOptions) (*InstallResult, error) {
	result := &InstallResult{
		Errors: make(map[string]string),
	}

	// Determine which policies to install
	var policyNames []string
	bundleName := ""
	seen := make(map[string]bool)

	// If bundle is specified, resolve bundle policies first
	if opts.Bundle != "" {
		bundleName = opts.Bundle
		bundlePolicies, err := cat.ResolveBundlePolicies(opts.Bundle)
		if err != nil {
			return nil, fmt.Errorf("resolving bundle policies: %w", err)
		}
		for _, p := range bundlePolicies {
			if !seen[p] {
				seen[p] = true
				policyNames = append(policyNames, p)
			}
		}
	}

	// Add any additional positional policies (deduplicated)
	// These are installed as template-only (no constraints) even when bundle is set
	for _, p := range opts.Policies {
		if !seen[p] {
			seen[p] = true
			policyNames = append(policyNames, p)
		}
	}

	if len(policyNames) == 0 {
		return nil, fmt.Errorf("no policies specified")
	}

	// Track total policies requested
	result.TotalRequested = len(policyNames)

	// Validate Gatekeeper is installed (skip if dry-run)
	if !opts.DryRun {
		installed, err := k8sClient.GatekeeperInstalled(ctx)
		if err != nil {
			return nil, fmt.Errorf("checking Gatekeeper installation: %w", err)
		}
		if !installed {
			return nil, &GatekeeperNotInstalledError{}
		}
	}

	// Build set of bundle policies for determining if constraints should be installed
	bundlePolicies := make(map[string]bool)
	if opts.Bundle != "" {
		bundlePolicyList, _ := cat.ResolveBundlePolicies(opts.Bundle)
		for _, p := range bundlePolicyList {
			bundlePolicies[p] = true
		}
	}

	// Install each policy
	for _, policyName := range policyNames {
		policy := cat.GetPolicy(policyName)
		if policy == nil {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = fmt.Sprintf("policy not found: %s", policyName)
			// Fail fast per MVP design
			return result, nil
		}

		// Determine if this policy should install constraints
		// Only bundle policies get constraints, additional positional policies get template-only
		installBundle := ""
		if bundlePolicies[policyName] {
			installBundle = bundleName
		}

		skipped, err := installPolicy(ctx, k8sClient, fetcher, policy, installBundle, opts, result)
		if err != nil {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = err.Error()
			// Preserve typed error for conflict detection
			var conflictErr *ConflictError
			if errors.As(err, &conflictErr) {
				result.ConflictErr = conflictErr
			}
			// Fail fast - stop on first error
			return result, nil
		}
		if skipped {
			result.Skipped = append(result.Skipped, policyName)
		} else {
			result.Installed = append(result.Installed, policyName)
			result.TemplatesInstalled++
		}
	}

	return result, nil
}

func installPolicy(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, policy *catalog.Policy, bundleName string, opts *InstallOptions, result *InstallResult) (skipped bool, err error) {
	// Fetch template YAML
	templateData, err := fetcher.FetchContent(ctx, policy.TemplatePath)
	if err != nil {
		return false, fmt.Errorf("fetching template: %w", err)
	}

	// Parse template
	template := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(templateData, &template.Object); err != nil {
		return false, fmt.Errorf("parsing template YAML: %w", err)
	}

	// Check for existing template
	templateAlreadyInstalled := false
	if !opts.DryRun {
		existing, err := k8sClient.GetTemplate(ctx, template.GetName())
		if err == nil {
			// Template exists - check if managed by gator
			if !labels.IsManagedByGator(existing) {
				return false, &ConflictError{
					ResourceKind: "ConstraintTemplate",
					ResourceName: template.GetName(),
				}
			}
			// Check if same version
			existingVersion := labels.GetPolicyVersion(existing)
			if existingVersion == policy.Version {
				templateAlreadyInstalled = true
			}
		}
	}

	// Add labels and annotations
	labels.AddManagedLabels(template, policy.Version, bundleName)

	// Install or update template if not already at same version
	if !opts.DryRun && !templateAlreadyInstalled {
		if err := k8sClient.InstallTemplate(ctx, template); err != nil {
			return false, fmt.Errorf("installing template: %w", err)
		}
	}

	// Install constraint if bundle and constraintPath exists
	if bundleName != "" && policy.ConstraintPath != "" {
		if err := installConstraint(ctx, k8sClient, fetcher, policy, bundleName, opts, result, template); err != nil {
			return false, err
		}
	}

	// Return whether this policy was skipped (already at same version)
	if templateAlreadyInstalled && (bundleName == "" || policy.ConstraintPath == "") {
		return true, nil
	}

	return false, nil
}

func installConstraint(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, policy *catalog.Policy, bundleName string, opts *InstallOptions, result *InstallResult, template *unstructured.Unstructured) error {
	constraintData, err := fetcher.FetchContent(ctx, policy.ConstraintPath)
	if err != nil {
		return fmt.Errorf("fetching constraint: %w", err)
	}

	constraint := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(constraintData, &constraint.Object); err != nil {
		return fmt.Errorf("parsing constraint YAML: %w", err)
	}

	// Override enforcement action if specified
	if opts.EnforcementAction != "" {
		if err := setEnforcementAction(constraint, opts.EnforcementAction); err != nil {
			return err
		}
	}

	// Add labels
	labels.AddManagedLabels(constraint, policy.Version, bundleName)

	// Install constraint
	if !opts.DryRun {
		// Wait for the template status to show created=true
		// Use the template name from the parsed YAML, not the catalog policy name
		if err := k8sClient.WaitForTemplateReady(ctx, template.GetName(), DefaultReconcileTimeout); err != nil {
			return fmt.Errorf("waiting for template ready: %w", err)
		}
		// Wait for the constraint CRD to be available (created by Gatekeeper after the template is installed)
		// Gatekeeper needs time to reconcile the ConstraintTemplate and generate the CRD
		if err := k8sClient.WaitForConstraintCRD(ctx, constraint.GetKind(), DefaultReconcileTimeout); err != nil {
			return fmt.Errorf("waiting for constraint CRD: %w", err)
		}

		if err := k8sClient.InstallConstraint(ctx, constraint); err != nil {
			return fmt.Errorf("installing constraint: %w", err)
		}
	}

	result.ConstraintsInstalled++
	return nil
}

func setEnforcementAction(constraint *unstructured.Unstructured, action string) error {
	spec, found, err := unstructured.NestedMap(constraint.Object, "spec")
	if err != nil {
		return fmt.Errorf("getting constraint spec: %w", err)
	}
	if !found {
		spec = make(map[string]interface{})
	}
	spec["enforcementAction"] = action
	return unstructured.SetNestedMap(constraint.Object, spec, "spec")
}

// GatekeeperNotInstalledError is returned when Gatekeeper CRDs are not found.
type GatekeeperNotInstalledError struct{}

func (e *GatekeeperNotInstalledError) Error() string {
	return `Gatekeeper CRDs not found in cluster.

Install Gatekeeper first:
  kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/deploy/gatekeeper.yaml

Or with Helm:
  helm install gatekeeper/gatekeeper --name-template=gatekeeper`
}

// ConflictError is returned when a resource exists but is not managed by gator.
type ConflictError struct {
	ResourceKind string
	ResourceName string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s '%s' already exists but is not managed by gator (missing label 'gatekeeper.sh/managed-by: gator')",
		e.ResourceKind, e.ResourceName)
}
