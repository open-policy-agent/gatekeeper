package policy

import (
	"errors"
	"fmt"
	"os"

	gatorpolicy "github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/output"
	"github.com/spf13/cobra"
)

var (
	uninstallBundles []string
	uninstallDryRun  bool
	uninstallOutput  string
)

func newUninstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall <policy...>",
		Short: "Remove policies from the cluster",
		Long: `Remove Gatekeeper policies that were installed by gator.

This removes the ConstraintTemplate. Kubernetes will automatically remove
any Constraints that use the template when the CRD is deleted.`,
		Example: `# Uninstall a policy
gator policy uninstall k8srequiredlabels

# Uninstall multiple policies
gator policy uninstall k8srequiredlabels k8scontainerlimits

# Uninstall all policies in a bundle
gator policy uninstall --bundle pod-security-baseline

# Preview changes without applying
gator policy uninstall k8srequiredlabels --dry-run

# Output as JSON for scripting
gator policy uninstall k8srequiredlabels -o json`,
		RunE: runUninstall,
	}

	cmd.Flags().StringSliceVar(&uninstallBundles, "bundle", nil, "Uninstall all policies in a bundle (may be specified multiple times)")
	cmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Preview changes without applying (requires cluster access to check current state)")
	cmd.Flags().StringVarP(&uninstallOutput, "output", "o", "table", "Output format: table, json")

	return cmd
}

func runUninstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Validate arguments
	if len(uninstallBundles) == 0 && len(args) == 0 {
		return fmt.Errorf("specify policy name(s) or use --bundle to uninstall a bundle")
	}

	// Create printer
	printer, err := output.NewPrinter(output.Format(uninstallOutput))
	if err != nil {
		return err
	}

	// Resolve bundle policies via catalog if --bundle is specified
	policyNames := args
	if len(uninstallBundles) > 0 {
		cache, err := catalog.NewCache()
		if err != nil {
			return fmt.Errorf("initializing cache: %w", err)
		}

		cat, err := cache.LoadCatalog()
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nRun 'gator policy update' to refresh the catalog.")
			return fmt.Errorf("loading catalog: %w", err)
		}

		seen := make(map[string]bool)
		for _, name := range policyNames {
			seen[name] = true
		}
		for _, b := range uninstallBundles {
			bundlePolicies, err := cat.ResolveBundlePolicies(b)
			if err != nil {
				return err
			}
			for _, name := range bundlePolicies {
				if !seen[name] {
					seen[name] = true
					policyNames = append(policyNames, name)
				}
			}
		}
	}

	// Create Kubernetes client
	k8sClient, err := client.NewK8sClient()
	if err != nil {
		return fmt.Errorf("creating Kubernetes client: %w", err)
	}

	// Build uninstall options
	opts := client.UninstallOptions{
		Policies: policyNames,
		DryRun:   uninstallDryRun,
	}

	// Perform uninstallation
	result, err := client.Uninstall(ctx, k8sClient, opts)
	if err != nil {
		// Check for specific error types
		gatekeeperNotInstalledError := &client.GatekeeperNotInstalledError{}
		if errors.As(err, &gatekeeperNotInstalledError) {
			fmt.Fprintln(os.Stderr, err.Error())
			return gatorpolicy.NewClusterError(err.Error())
		}
		return err
	}

	// Build output result
	outResult := &output.UninstallResult{
		Uninstalled: result.Uninstalled,
		NotFound:    result.NotFound,
		NotManaged:  result.NotManaged,
		DryRun:      uninstallDryRun,
	}

	for _, name := range result.Failed {
		outResult.Failed = append(outResult.Failed, output.FailedEntry{
			Name:  name,
			Error: result.Errors[name],
		})
	}

	// Print results
	if printErr := printer.PrintUninstallResult(os.Stdout, outResult); printErr != nil {
		return printErr
	}

	// Return appropriate error for non-success cases
	if len(result.NotManaged) > 0 {
		return gatorpolicy.NewConflictError("uninstall failed: some policies are not managed by gator")
	}

	if len(result.Failed) > 0 {
		return gatorpolicy.NewPartialSuccessError("uninstall incomplete: some policies failed")
	}

	return nil
}
