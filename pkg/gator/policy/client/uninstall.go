package client

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/labels"
	"k8s.io/apimachinery/pkg/api/errors"
)

// UninstallOptions contains options for uninstalling policies.
type UninstallOptions struct {
	// Policies is the list of policy names to uninstall.
	Policies []string
	// DryRun if true, only prints what would be done.
	DryRun bool
}

// UninstallResult contains the result of an uninstall operation.
type UninstallResult struct {
	// Uninstalled is the list of successfully uninstalled policies.
	Uninstalled []string
	// NotFound is the list of policies that were not found.
	NotFound []string
	// NotManaged is the list of policies that exist but are not managed by gator.
	NotManaged []string
	// Failed is the list of policies that failed to uninstall.
	Failed []string
	// Errors contains error messages for failed policies.
	Errors map[string]string
}

// Uninstall removes policies from the cluster.
func Uninstall(ctx context.Context, k8sClient Client, opts UninstallOptions) (*UninstallResult, error) {
	result := &UninstallResult{
		Errors: make(map[string]string),
	}

	if len(opts.Policies) == 0 {
		return nil, fmt.Errorf("no policies specified")
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

	// Uninstall each policy - fail fast on errors per design doc
	for _, policyName := range opts.Policies {
		err := uninstallPolicy(ctx, k8sClient, policyName, opts.DryRun, result)
		if err != nil {
			result.Failed = append(result.Failed, policyName)
			result.Errors[policyName] = err.Error()
			// Fail fast - stop on first error
			return result, err
		}
	}

	return result, nil
}

func uninstallPolicy(ctx context.Context, k8sClient Client, policyName string, dryRun bool, result *UninstallResult) error {
	// Get existing template
	existing, err := k8sClient.GetTemplate(ctx, policyName)
	if err != nil {
		// Distinguish between "not found" and other errors (auth, network, etc.)
		if errors.IsNotFound(err) {
			result.NotFound = append(result.NotFound, policyName)
			return nil // Not found is not an error for uninstall
		}
		// Real error - propagate it
		return fmt.Errorf("getting template: %w", err)
	}

	// Check if managed by gator
	if !labels.IsManagedByGator(existing) {
		result.NotManaged = append(result.NotManaged, policyName)
		return &ConflictError{
			ResourceKind: "ConstraintTemplate",
			ResourceName: policyName,
		}
	}

	// Delete template
	// Note: When the ConstraintTemplate is deleted, Gatekeeper removes the associated
	// Constraint CRD. Kubernetes garbage-collects any Constraint CRs when the CRD is deleted.
	if !dryRun {
		if err := k8sClient.DeleteTemplate(ctx, policyName); err != nil {
			return fmt.Errorf("deleting template: %w", err)
		}
	}

	result.Uninstalled = append(result.Uninstalled, policyName)
	return nil
}
