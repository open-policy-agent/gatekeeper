package policy

import (
	"errors"
	"fmt"
	"os"

	gatorpolicy "github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
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

# Output results as JSON
gator policy uninstall k8srequiredlabels -o json`,
		RunE: runUninstall,
	}

	cmd.Flags().StringSliceVar(&uninstallBundles, "bundle", nil, "Uninstall all policies in a bundle (may be specified multiple times)")
	cmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Preview changes without applying (requires cluster access — uses server-side dry run)")
	cmd.Flags().StringVarP(&uninstallOutput, "output", "o", "", "Output format: table (default) or json")

	return cmd
}

func runUninstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Validate arguments
	if len(uninstallBundles) == 0 && len(args) == 0 {
		return fmt.Errorf("specify policy name(s) or use --bundle to uninstall a bundle")
	}

	// Validate output format early
	if uninstallOutput != "" && uninstallOutput != "table" && uninstallOutput != "json" {
		return fmt.Errorf("invalid output format: %s (must be table or json)", uninstallOutput)
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

	// Print header
	if len(uninstallBundles) > 0 {
		for _, b := range uninstallBundles {
			if uninstallDryRun {
				fmt.Fprintf(os.Stdout, "==> Would uninstall %s bundle:\n", b)
			} else {
				fmt.Fprintf(os.Stdout, "Uninstalling %s bundle...\n", b)
			}
		}
	} else if uninstallDryRun {
		fmt.Fprintf(os.Stdout, "==> Would uninstall %d policies:\n", len(policyNames))
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

	// Print results
	for _, name := range result.Uninstalled {
		if uninstallDryRun {
			fmt.Fprintf(os.Stdout, "%s\n", name)
		} else {
			fmt.Fprintf(os.Stdout, "✓ %s uninstalled\n", name)
		}
	}

	for _, name := range result.NotFound {
		fmt.Fprintf(os.Stdout, "- %s (not found)\n", name)
	}

	// Print failures
	hasConflict := false
	for _, name := range result.NotManaged {
		errMsg := result.Errors[name]
		fmt.Fprintf(os.Stderr, "✗ %s - %s\n", name, errMsg)
		hasConflict = true
	}

	for _, name := range result.Failed {
		errMsg := result.Errors[name]
		fmt.Fprintf(os.Stderr, "✗ %s - failed: %s\n", name, errMsg)
	}

	if hasConflict {
		return gatorpolicy.NewConflictError("uninstall failed: some policies are not managed by gator")
	}

	if len(result.Failed) > 0 {
		return gatorpolicy.NewPartialSuccessError("uninstall incomplete: some policies failed")
	}

	return nil
}
