package client

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
)

// UpgradeOptions contains options for upgrading policies.
type UpgradeOptions struct {
	// Policies is the list of policy names to upgrade (empty with All=true upgrades all).
	Policies []string
	// All if true, upgrades all installed policies.
	All bool
	// EnforcementAction overrides the enforcement action for constraints.
	EnforcementAction string
	// DryRun if true, only prints what would be done.
	DryRun bool
	// Force if true, bypasses the cluster Kubernetes version compatibility check.
	Force bool
}

// UpgradeResult contains the result of an upgrade operation.
type UpgradeResult struct {
	// Upgraded is the list of successfully upgraded policies with their version changes.
	Upgraded []VersionChange
	// AlreadyCurrent is the list of policies already at the latest version.
	AlreadyCurrent []string
	// NotFound is the list of policies not found in the catalog.
	NotFound []string
	// NotInstalled is the list of policies not installed in the cluster.
	NotInstalled []string
	// Incompatible is the list of policies skipped due to Kubernetes version incompatibility.
	Incompatible []IncompatibleEntry
	// Failed is the list of policies that failed to upgrade.
	Failed []string
	// Errors contains error messages for failed policies.
	Errors map[string]string
}

// VersionChange represents a version change for a policy.
type VersionChange struct {
	Name        string
	FromVersion string
	ToVersion   string
}

// Upgrade upgrades installed policies to their latest versions.
func Upgrade(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, cat *catalog.PolicyCatalog, opts UpgradeOptions) (*UpgradeResult, error) {
	result := &UpgradeResult{
		Errors: make(map[string]string),
	}

	if !opts.All && len(opts.Policies) == 0 {
		return nil, fmt.Errorf("specify policy name(s) or use --all to upgrade all policies")
	}

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

	// Get list of installed policies
	installedPolicies, err := k8sClient.ListManagedTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing installed policies: %w", err)
	}

	// Build map of installed policies
	installedMap := make(map[string]InstalledPolicy)
	for _, p := range installedPolicies {
		installedMap[p.Name] = p
	}

	// Determine which policies to upgrade
	var policyNames []string
	if opts.All {
		for _, p := range installedPolicies {
			policyNames = append(policyNames, p.Name)
		}
	} else {
		policyNames = opts.Policies
	}

	// Resolve the cluster version once for the whole batch; each upgradePolicy
	// call reuses it rather than issuing its own discovery request per policy.
	// serverVersion stays empty when the gate is off (dry-run, --force, or no
	// candidate policy declares a bound).
	var serverVersion string
	if !opts.DryRun && !opts.Force && anyPolicyHasVersionBounds(cat, policyNames) {
		serverVersion, err = k8sClient.ServerVersion(ctx)
		if err != nil {
			return nil, fmt.Errorf("determining cluster Kubernetes version (use --force to skip the compatibility check): %w", err)
		}
	}

	// Upgrade each policy
	for _, policyName := range policyNames {
		installed, found := installedMap[policyName]
		if !found {
			result.NotInstalled = append(result.NotInstalled, policyName)
			continue
		}

		policy := cat.GetPolicy(policyName)
		if policy == nil {
			result.NotFound = append(result.NotFound, policyName)
			continue
		}

		// Check if already at latest version
		if installed.Version == policy.Version {
			result.AlreadyCurrent = append(result.AlreadyCurrent, policyName)
			continue
		}

		// Upgrade the policy
		incompatible, err := upgradePolicy(ctx, k8sClient, fetcher, policy, installed.Bundle, opts, serverVersion)
		if err != nil {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = err.Error()
			// Fail fast
			return result, nil
		}
		if incompatible != nil {
			// A policy incompatible with the cluster version is skipped, not a
			// hard failure: continue so the remaining policies still upgrade.
			result.Incompatible = append(result.Incompatible, *incompatible)
			continue
		}

		result.Upgraded = append(result.Upgraded, VersionChange{
			Name:        policyName,
			FromVersion: installed.Version,
			ToVersion:   policy.Version,
		})
	}

	return result, nil
}

// upgradePolicy upgrades a single policy. It returns a non-nil *IncompatibleEntry
// (and nil error) when the policy is skipped because the cluster's Kubernetes
// version is outside the policy's supported range; a non-nil error indicates a
// genuine failure. serverVersion is the pre-resolved cluster version (empty when
// the compatibility gate is off).
func upgradePolicy(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, policy *catalog.Policy, bundleName string, opts UpgradeOptions, serverVersion string) (*IncompatibleEntry, error) {
	// Use install with the existing bundle name to preserve constraint installation behavior
	var bundles []string
	if bundleName != "" {
		bundles = []string{bundleName}
	}
	installOpts := &InstallOptions{
		Policies:          []string{policy.Name},
		Bundles:           bundles, // Pass bundle context so constraints are upgraded too
		EnforcementAction: opts.EnforcementAction,
		DryRun:            opts.DryRun,
		Force:             opts.Force,
		serverVersion:     serverVersion, // reuse the batch-resolved cluster version
	}

	// Create a minimal catalog for the install
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{*policy},
	}

	// If this was a bundle-installed policy, we need to ensure constraints are updated
	// by temporarily including this policy in the bundle's policy list
	if bundleName != "" {
		cat.Bundles = []catalog.Bundle{
			{
				Name:     bundleName,
				Policies: []string{policy.Name},
			},
		}
	}

	installResult, err := Install(ctx, k8sClient, fetcher, cat, installOpts)
	if err != nil {
		return nil, err
	}

	// Install reports a Kubernetes-version-incompatible policy as a skip
	// (nil error), not a failure. Surface it to the caller so the upgrade is not
	// falsely recorded as successful, without treating it as a hard failure.
	for i := range installResult.Incompatible {
		if installResult.Incompatible[i].Name == policy.Name {
			return &installResult.Incompatible[i], nil
		}
	}

	// Install also records a per-policy failure (e.g. an unparseable version
	// bound) in Failed/Errors with a nil top-level error. Surface it as a
	// genuine error so the upgrade is not falsely reported as successful.
	if msg, failed := installResult.Errors[policy.Name]; failed {
		return nil, fmt.Errorf("installing %s: %s", policy.Name, msg)
	}

	return nil, nil
}

// GetUpgradableCount returns the count of policies that have updates available.
func GetUpgradableCount(installed []InstalledPolicy, cat *catalog.PolicyCatalog) int {
	count := 0
	for _, p := range installed {
		policy := cat.GetPolicy(p.Name)
		if policy != nil && policy.Version != p.Version {
			count++
		}
	}
	return count
}

// GetUpgradablePolicies returns a list of policies that have updates available.
func GetUpgradablePolicies(installed []InstalledPolicy, cat *catalog.PolicyCatalog) []VersionChange {
	var changes []VersionChange
	for _, p := range installed {
		policy := cat.GetPolicy(p.Name)
		if policy != nil && policy.Version != p.Version {
			changes = append(changes, VersionChange{
				Name:        p.Name,
				FromVersion: p.Version,
				ToVersion:   policy.Version,
			})
		}
	}
	return changes
}
