package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/labels"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	// Bundles is the list of bundle names to install.
	Bundles []string
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
	seen := make(map[string]bool)
	// policyBundle tracks which bundle a policy was resolved from (first match wins).
	policyBundle := make(map[string]string)

	// If bundles are specified, resolve bundle policies first
	for _, bundleName := range opts.Bundles {
		bundlePolicies, err := cat.ResolveBundlePolicies(bundleName)
		if err != nil {
			return nil, fmt.Errorf("resolving bundle policies: %w", err)
		}
		for _, p := range bundlePolicies {
			if !seen[p] {
				seen[p] = true
				policyNames = append(policyNames, p)
				policyBundle[p] = bundleName
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

	// Install each policy
	for _, policyName := range policyNames {
		policy := cat.GetPolicy(policyName)
		if policy == nil {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = fmt.Sprintf("policy not found: %s", policyName)
			// Fail fast per MVP design
			return result, nil
		}

		// Determine if this policy should install constraints.
		// Only bundle-resolved policies get constraints; positional policies get template-only.
		installBundle := policyBundle[policyName]

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
		} else if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("checking existing template: %w", err)
		}
	}

	// Add labels and annotations
	labels.AddManagedLabels(template, policy.Version, bundleName, catalog.DefaultRepository)

	// Install or update template if not already at same version
	if !opts.DryRun && !templateAlreadyInstalled {
		if err := k8sClient.InstallTemplate(ctx, template); err != nil {
			return false, fmt.Errorf("installing template: %w", err)
		}
	}

	// Install constraint if bundle has a constraint path defined
	constraintPath := policy.BundleConstraints[bundleName]
	if bundleName != "" && constraintPath != "" {
		if err := installConstraint(ctx, k8sClient, fetcher, policy, constraintPath, bundleName, opts, result, template); err != nil {
			return false, err
		}
	}

	// Return whether this policy was skipped (already at same version)
	if templateAlreadyInstalled && (bundleName == "" || constraintPath == "") {
		return true, nil
	}

	return false, nil
}

func installConstraint(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, policy *catalog.Policy, constraintPath string, bundleName string, opts *InstallOptions, result *InstallResult, template *unstructured.Unstructured) error {
	constraintData, err := fetcher.FetchContent(ctx, constraintPath)
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
	labels.AddManagedLabels(constraint, policy.Version, bundleName, catalog.DefaultRepository)

	// Install constraint
	if !opts.DryRun {
		// Wait for the template status to show created=true
		if err := k8sClient.WaitForTemplateReady(ctx, template.GetName(), DefaultReconcileTimeout); err != nil {
			return fmt.Errorf("waiting for template ready: %w", err)
		}
		// Wait for the constraint CRD to be available
		if err := k8sClient.WaitForConstraintCRD(ctx, constraint.GetKind(), DefaultReconcileTimeout); err != nil {
			return fmt.Errorf("waiting for constraint CRD: %w", err)
		}

		// Check if constraint already exists and is not managed by gator
		gvr := constraintGVR(constraint.GetKind())
		existing, err := k8sClient.GetConstraint(ctx, gvr, constraint.GetName())
		if err == nil {
			if !labels.IsManagedByGator(existing) {
				return &ConflictError{
					ResourceKind: constraint.GetKind(),
					ResourceName: constraint.GetName(),
				}
			}
			// Preserve existing enforcement action if not explicitly overridden.
			// This ensures upgrades don't silently revert a user's enforcement setting.
			if opts.EnforcementAction == "" {
				existingAction, _, _ := unstructured.NestedString(existing.Object, "spec", "enforcementAction")
				if existingAction != "" {
					if err := setEnforcementAction(constraint, existingAction); err != nil {
						return err
					}
				}
			}
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

// constraintGVR returns the GroupVersionResource for a constraint kind.
func constraintGVR(kind string) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: strings.ToLower(kind),
	}
}

// GatekeeperNotInstalledError is returned when Gatekeeper CRDs are not found.
type GatekeeperNotInstalledError struct{}

func (e *GatekeeperNotInstalledError) Error() string {
	return `Gatekeeper CRDs not found in cluster.

See the installation guide: https://open-policy-agent.github.io/gatekeeper/website/docs/install`
}

// ConflictError is returned when a resource exists but is not managed by gator.
type ConflictError struct {
	ResourceKind string
	ResourceName string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s '%s' already exists but is not managed by gator (expected label 'gatekeeper.sh/managed-by: gator' and annotation 'gatekeeper.sh/policy-source')",
		e.ResourceKind, e.ResourceName)
}
