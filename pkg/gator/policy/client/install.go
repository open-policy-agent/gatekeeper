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
	// Force if true, bypasses the cluster Kubernetes version compatibility check.
	Force bool
}

// IncompatibleEntry describes a policy skipped because the cluster's Kubernetes
// version falls outside the policy's supported range.
type IncompatibleEntry struct {
	// Name is the policy name.
	Name string `json:"name"`
	// Reason is a human-readable explanation of the incompatibility.
	Reason string `json:"reason"`
}

// InstallResult contains the result of an install operation.
type InstallResult struct {
	// Installed is the list of successfully installed policies.
	Installed []string
	// Skipped is the list of skipped policies (already at same version).
	Skipped []string
	// Incompatible is the list of policies skipped due to Kubernetes version incompatibility.
	Incompatible []IncompatibleEntry
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
	return install(ctx, k8sClient, fetcher, cat, opts, "")
}

// install is Install's implementation, taking an additional pre-resolved
// cluster Kubernetes version. It lets a batch caller (e.g. Upgrade) resolve
// the version once for a whole batch instead of once per policy. Install
// itself always resolves the version internally, passing "".
func install(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, cat *catalog.PolicyCatalog, opts *InstallOptions, preResolvedServerVersion string) (*InstallResult, error) {
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

	// Validate Gatekeeper is installed. This is a real-run concern only; a
	// dry-run just previews and does not require Gatekeeper to be present.
	if !opts.DryRun {
		installed, err := k8sClient.GatekeeperInstalled(ctx)
		if err != nil {
			return nil, fmt.Errorf("checking Gatekeeper installation: %w", err)
		}
		if !installed {
			return nil, &GatekeeperNotInstalledError{}
		}
	}

	// Resolve the cluster version for the Kubernetes-version compatibility gate.
	// This runs for dry-run too so the preview reflects the same gate a real
	// install applies. serverVersion stays empty when the gate is off (--force or
	// no bounded policy), disabling the per-policy version check below. A caller
	// (e.g. Upgrade) may pass a pre-resolved version to avoid re-querying.
	serverVersion, err := resolveGateServerVersion(ctx, k8sClient, opts.Force, anyPolicyHasVersionBounds(cat, policyNames), preResolvedServerVersion)
	if err != nil {
		return nil, err
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

		// The Kubernetes-version compatibility gate is applied inside installPolicy,
		// after it determines whether a write would actually occur: an idempotent
		// reinstall of a policy already at the target version is a no-op and must
		// not be reported as incompatible on an out-of-range cluster. serverVersion
		// is empty when the gate is disabled (--force or no bounded policy).
		skipped, incompatible, err := installPolicy(ctx, k8sClient, fetcher, policy, installBundle, opts, result, serverVersion)
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
		if incompatible != nil {
			result.Incompatible = append(result.Incompatible, *incompatible)
			continue
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

// policyHasVersionBounds reports whether a policy declares a minimum or maximum
// Kubernetes version, i.e. whether the compatibility gate can fire for it.
func policyHasVersionBounds(p *catalog.Policy) bool {
	return p != nil && (p.MinKubernetesVersion != "" || p.MaxKubernetesVersion != "")
}

// anyPolicyHasVersionBounds reports whether any of the named policies declares a
// minimum or maximum Kubernetes version, i.e. whether the compatibility gate can
// fire at all for this install.
func anyPolicyHasVersionBounds(cat *catalog.PolicyCatalog, names []string) bool {
	for _, name := range names {
		if policyHasVersionBounds(cat.GetPolicy(name)) {
			return true
		}
	}
	return false
}

// resolveGateServerVersion resolves the cluster Kubernetes version used by the
// compatibility gate, shared by Install and Upgrade. It returns "" (gate
// disabled) when force is set or hasBounds is false. A non-empty preResolved
// version is used as-is instead of querying the cluster, letting a caller (e.g.
// Upgrade) resolve the version once for a whole batch.
//
// --force is the single, explicit fail-open path: it disables the gate up front.
// Otherwise, when compatibility genuinely cannot be established the caller is
// given an actionable error rather than proceeding blindly. A cluster-version
// that does not parse is such a case (bounded policies must not install as
// "compatible" against a version the gate cannot reason about), as is a
// cluster-connectivity failure while discovering the version.
func resolveGateServerVersion(ctx context.Context, k8sClient Client, force, hasBounds bool, preResolved string) (string, error) {
	if force || !hasBounds {
		return "", nil
	}
	serverVersion := preResolved
	if serverVersion == "" {
		v, err := k8sClient.ServerVersion(ctx)
		if err != nil {
			return "", fmt.Errorf("determining cluster Kubernetes version: the cluster must be reachable to check policy compatibility (use --force to skip the compatibility check): %w", err)
		}
		serverVersion = v
	}
	if err := catalog.ValidateK8sVersion(serverVersion); err != nil {
		return "", fmt.Errorf("cluster Kubernetes version %q could not be parsed, so policy compatibility cannot be verified; use --force to install without the compatibility check: %w", serverVersion, err)
	}
	return serverVersion, nil
}

func installPolicy(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, policy *catalog.Policy, bundleName string, opts *InstallOptions, result *InstallResult, serverVersion string) (skipped bool, incompatible *IncompatibleEntry, err error) {
	// Fetch template YAML
	templateData, err := fetcher.FetchContent(ctx, policy.TemplatePath)
	if err != nil {
		return false, nil, fmt.Errorf("fetching template: %w", err)
	}

	// Parse template
	template := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(templateData, &template.Object); err != nil {
		return false, nil, fmt.Errorf("parsing template YAML: %w", err)
	}

	// Check for existing template
	templateAlreadyInstalled := false
	if !opts.DryRun {
		existing, err := k8sClient.GetTemplate(ctx, template.GetName())
		if err == nil {
			// Template exists - check if managed by gator
			if !labels.IsManagedByGator(existing) {
				return false, nil, &ConflictError{
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
			return false, nil, fmt.Errorf("checking existing template: %w", err)
		}
	}

	// A bundle policy with a constraint path always upserts its constraint.
	constraintPath := policy.BundleConstraints[bundleName]
	hasConstraint := bundleName != "" && constraintPath != ""

	// A template-only policy already managed at the target version is a pure
	// no-op: nothing would be written to the cluster.
	isNoOp := templateAlreadyInstalled && !hasConstraint

	// Determine whether applying this policy would write anything to the cluster.
	// Dry-run does not read existing state, so it always evaluates the gate so the
	// preview matches a real install.
	wouldWrite := opts.DryRun || !isNoOp

	// Kubernetes-version compatibility gate. It is applied only to policies that
	// would actually write, mirroring Upgrade (which classifies already-current
	// policies before gating): an idempotent reinstall must not be reported as
	// incompatible on an out-of-range cluster when no write would occur.
	// serverVersion is non-empty only when the gate is enabled, and it was
	// validated in resolveGateServerVersion, so a parse error here can only come
	// from a malformed minKubernetesVersion / maxKubernetesVersion on the policy
	// itself. Fail such a policy rather than fail open, since ParseCatalog does not
	// run schema validation and cached/custom catalogs can carry bad bounds.
	// --force disables the gate by leaving serverVersion empty.
	if serverVersion != "" && wouldWrite {
		inRange, verr := catalog.K8sVersionInRange(serverVersion, policy.MinKubernetesVersion, policy.MaxKubernetesVersion)
		if verr != nil {
			return false, nil, fmt.Errorf("evaluating Kubernetes version compatibility: %w", verr)
		}
		if !inRange {
			return false, &IncompatibleEntry{
				Name: policy.Name,
				Reason: fmt.Sprintf("cluster Kubernetes version %s is outside the supported range %s",
					serverVersion, catalog.FormatK8sVersionRange(policy.MinKubernetesVersion, policy.MaxKubernetesVersion)),
			}, nil
		}
	}

	// Add labels and annotations
	labels.AddManagedLabels(template, policy.Version, bundleName, catalog.DefaultRepository)

	// Install or update template if not already at same version
	if !opts.DryRun && !templateAlreadyInstalled {
		if err := k8sClient.InstallTemplate(ctx, template); err != nil {
			return false, nil, fmt.Errorf("installing template: %w", err)
		}
	}

	// Install constraint if bundle has a constraint path defined
	if hasConstraint {
		if err := installConstraint(ctx, k8sClient, fetcher, policy, constraintPath, bundleName, opts, result, template); err != nil {
			return false, nil, err
		}
	}

	// Return whether this policy was skipped (already at same version)
	if isNoOp {
		return true, nil, nil
	}

	return false, nil, nil
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
