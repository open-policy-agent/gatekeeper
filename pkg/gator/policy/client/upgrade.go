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

	// Classify each policy first, recording the ones that are not installed, not
	// in the catalog, or already current. Only policies that will actually be
	// upgraded are collected as candidates below.
	type upgradeCandidate struct {
		policy    *catalog.Policy
		installed InstalledPolicy
	}
	var candidates []upgradeCandidate
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

		candidates = append(candidates, upgradeCandidate{policy: policy, installed: installed})
	}

	// Resolve the cluster version once for the whole batch; each upgradePolicy
	// call reuses it rather than issuing its own discovery request per policy.
	// This runs during dry-run too: dry-run has cluster access, and applying the
	// gate keeps the preview accurate (an incompatible policy is shown as skipped,
	// matching what a real upgrade would do). Gate only on the policies actually
	// being upgraded, so a batch with no bounded candidates never contacts the
	// cluster for a version.
	anyBounded := false
	for _, c := range candidates {
		if policyHasVersionBounds(c.policy) {
			anyBounded = true
			break
		}
	}

	// A failure here is not fatal to the whole batch: it only prevents gating
	// policies that declare a version bound. Those are recorded as failed in the
	// loop below while unbounded policies still upgrade, matching the per-policy
	// continue-on-error behavior.
	// allowQuery is true: upgrade always has cluster access (it must read the
	// installed versions), so it resolves the version even during dry-run and
	// passes it into install as a pre-resolved value, keeping the preview accurate.
	serverVersion, gateErr := resolveGateServerVersion(ctx, k8sClient, opts.Force, anyBounded, true, "")

	// Upgrade each candidate policy
	for _, c := range candidates {
		policy := c.policy
		installed := c.installed
		policyName := policy.Name

		// The cluster version is only needed to gate policies with version
		// bounds. If it could not be resolved, fail just those and let unbounded
		// policies upgrade normally.
		if gateErr != nil && policyHasVersionBounds(policy) {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = gateErr.Error()
			continue
		}

		// Upgrade the policy
		incompatible, upgraded, err := upgradePolicy(ctx, k8sClient, fetcher, policy, installed.Bundle, opts, serverVersion)
		if err != nil {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = err.Error()
			// Fail fast per MVP design, matching install
			return result, nil
		}
		if incompatible != nil {
			// A policy incompatible with the cluster version is skipped, not a
			// hard failure: continue so the remaining policies still upgrade.
			result.Incompatible = append(result.Incompatible, *incompatible)
			continue
		}
		if !upgraded {
			// Install skipped the policy because the cluster template was already
			// at the target version (e.g. a concurrent change between
			// classification and install). No version change happened, so record
			// it as already current rather than a fabricated upgrade.
			result.AlreadyCurrent = append(result.AlreadyCurrent, policyName)
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
// genuine failure. The returned bool reports whether the policy was actually
// upgraded: it is false when the install was skipped because the cluster was
// already at the target version. serverVersion is the pre-resolved cluster
// version (empty when the compatibility gate is off).
func upgradePolicy(ctx context.Context, k8sClient Client, fetcher catalog.Fetcher, policy *catalog.Policy, bundleName string, opts UpgradeOptions, serverVersion string) (*IncompatibleEntry, bool, error) {
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

	// Reuse the batch-resolved cluster version instead of re-querying per policy.
	installResult, err := install(ctx, k8sClient, fetcher, cat, installOpts, serverVersion)
	if err != nil {
		return nil, false, err
	}

	// The sub-catalog holds exactly this one policy, so Install reports its
	// outcome as a single entry; inspect the slices directly rather than
	// matching by name.

	// Install reports a Kubernetes-version-incompatible policy as a skip
	// (nil error), not a failure. Surface it to the caller so the upgrade is not
	// falsely recorded as successful, without treating it as a hard failure.
	if len(installResult.Incompatible) > 0 {
		return &installResult.Incompatible[0], false, nil
	}

	// Install also records a per-policy failure (e.g. an unparseable version
	// bound) in Failed/Errors with a nil top-level error. Surface it as a
	// genuine error so the upgrade is not falsely reported as successful.
	if len(installResult.Failed) > 0 {
		name := installResult.Failed[0]
		return nil, false, fmt.Errorf("installing %s: %s", name, installResult.Errors[name])
	}

	// Only report an upgrade when Install actually installed the policy. If the
	// cluster template was already at the target version (e.g. a concurrent
	// change), Install records it as skipped and no version change occurred.
	return nil, len(installResult.Installed) > 0, nil
}

// GetUpgradableCount returns the count of policies that have updates available
// and are compatible with the given cluster Kubernetes version. See
// GetUpgradablePolicies for how serverVersion is applied.
func GetUpgradableCount(installed []InstalledPolicy, cat *catalog.PolicyCatalog, serverVersion string) int {
	return len(GetUpgradablePolicies(installed, cat, serverVersion))
}

// GetUpgradablePolicies returns a list of policies that have updates available.
//
// A newer catalog version alone is not enough: `gator policy upgrade` skips a
// policy whose supported Kubernetes range excludes the cluster, so counting it
// here would overstate the upgrades that can actually be applied. serverVersion
// is the cluster's Kubernetes version used to filter out such policies. When it
// is empty (cluster version unknown) or a policy's bound is unparseable, the
// gate is skipped for that policy and the upgrade is still reported, so an
// undeterminable version never hides an available upgrade.
func GetUpgradablePolicies(installed []InstalledPolicy, cat *catalog.PolicyCatalog, serverVersion string) []VersionChange {
	var changes []VersionChange
	for _, p := range installed {
		policy := cat.GetPolicy(p.Name)
		if policy == nil || policy.Version == p.Version {
			continue
		}
		// Skip policies the cluster's Kubernetes version can't run: upgrade would
		// classify them as incompatible and skip them without --force.
		if serverVersion != "" && policyHasVersionBounds(policy) {
			inRange, err := catalog.K8sVersionInRange(serverVersion, policy.MinKubernetesVersion, policy.MaxKubernetesVersion)
			if err == nil && !inRange {
				continue
			}
		}
		changes = append(changes, VersionChange{
			Name:        p.Name,
			FromVersion: p.Version,
			ToVersion:   policy.Version,
		})
	}
	return changes
}
