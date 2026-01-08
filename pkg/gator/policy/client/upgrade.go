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
		err := upgradePolicy(ctx, k8sClient, fetcher, policy, installed.Bundle, opts)
		if err != nil {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = err.Error()
			// Fail fast
			return result, nil
		}

		result.Upgraded = append(result.Upgraded, VersionChange{
			Name:        policyName,
			FromVersion: installed.Version,
			ToVersion:   policy.Version,
		})
	}

	return result, nil
}

func upgradePolicy(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, policy *catalog.Policy, bundleName string, opts UpgradeOptions) error {
	// Use install with the existing bundle name
	installOpts := &InstallOptions{
		Policies:          []string{policy.Name},
		EnforcementAction: opts.EnforcementAction,
		DryRun:            opts.DryRun,
	}

	// Create a minimal catalog for the install
	cat := &catalog.PolicyCatalog{
		Policies: []catalog.Policy{*policy},
	}

	_, err := Install(ctx, k8sClient, fetcher, cat, installOpts)
	return err
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
