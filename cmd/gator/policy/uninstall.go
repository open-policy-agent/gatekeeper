package policy

import (
	"errors"
	"fmt"
	"os"

	gatorpolicy "github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/spf13/cobra"
)

var uninstallDryRun bool

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

# Preview changes without applying
gator policy uninstall k8srequiredlabels --dry-run`,
		Args: cobra.MinimumNArgs(1),
		RunE: runUninstall,
	}

	cmd.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Preview changes without applying")

	return cmd
}

func runUninstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Create Kubernetes client
	k8sClient, err := client.NewK8sClient()
	if err != nil {
		return fmt.Errorf("creating Kubernetes client: %w", err)
	}

	// Print header
	if uninstallDryRun {
		fmt.Fprintf(os.Stdout, "==> Would uninstall %d policies:\n", len(args))
	}

	// Build uninstall options
	opts := client.UninstallOptions{
		Policies: args,
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
