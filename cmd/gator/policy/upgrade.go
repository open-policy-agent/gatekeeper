package policy

import (
	"errors"
	"fmt"
	"os"
	"strings"

	gatorpolicy "github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/spf13/cobra"
)

var (
	upgradeAll               bool
	upgradeEnforcementAction string
	upgradeDryRun            bool
)

func newUpgradeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade [policy...]",
		Short: "Upgrade installed policies to latest versions",
		Long: `Upgrade installed policies to their latest versions from the catalog.

Requires specifying policy name(s) or --all flag.`,
		Example: `# Upgrade a specific policy
gator policy upgrade k8srequiredlabels

# Upgrade all installed policies
gator policy upgrade --all

# Preview changes without applying
gator policy upgrade --all --dry-run`,
		RunE: runUpgrade,
	}

	cmd.Flags().BoolVar(&upgradeAll, "all", false, "Upgrade all installed policies")
	cmd.Flags().StringVar(&upgradeEnforcementAction, "enforcement-action", "", "Override enforcement action (deny, warn, dryrun)")
	cmd.Flags().BoolVar(&upgradeDryRun, "dry-run", false, "Preview changes without applying")

	return cmd
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Validate arguments
	if !upgradeAll && len(args) == 0 {
		return fmt.Errorf("specify policy name(s) or use --all to upgrade all policies")
	}

	// Validate enforcement action
	if upgradeEnforcementAction != "" {
		action := strings.ToLower(upgradeEnforcementAction)
		if action != "deny" && action != "warn" && action != "dryrun" {
			return fmt.Errorf("invalid enforcement action: %s (must be deny, warn, or dryrun)", upgradeEnforcementAction)
		}
	}

	// Load catalog
	cache, err := catalog.NewCache()
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	cat, err := cache.LoadCatalog()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nRun 'gator policy update' to refresh the catalog.")
		return fmt.Errorf("loading catalog: %w", err)
	}

	// Create Kubernetes client
	k8sClient, err := client.NewK8sClient()
	if err != nil {
		return fmt.Errorf("creating Kubernetes client: %w", err)
	}

	// Create fetcher for templates/constraints with the catalog URL as base
	fetcher := catalog.NewHTTPFetcherWithBaseURL(catalog.DefaultTimeout, catalog.GetCatalogURL())

	// Print header (for dry-run, we print after we know what will be upgraded)

	// Build upgrade options
	opts := client.UpgradeOptions{
		Policies:          args,
		All:               upgradeAll,
		EnforcementAction: upgradeEnforcementAction,
		DryRun:            upgradeDryRun,
	}

	// Perform upgrade
	result, err := client.Upgrade(ctx, k8sClient, fetcher, cat, opts)
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
	if upgradeDryRun && len(result.Upgraded) > 0 {
		fmt.Fprintf(os.Stdout, "==> Would upgrade %d policies:\n", len(result.Upgraded))
	}
	for _, change := range result.Upgraded {
		if upgradeDryRun {
			fmt.Fprintf(os.Stdout, "%s %s -> %s\n", change.Name, change.FromVersion, change.ToVersion)
		} else {
			fmt.Fprintf(os.Stdout, "✓ %s upgraded (%s → %s)\n", change.Name, change.FromVersion, change.ToVersion)
		}
	}

	for _, name := range result.AlreadyCurrent {
		fmt.Fprintf(os.Stdout, "- %s (already at latest version)\n", name)
	}

	for _, name := range result.NotInstalled {
		fmt.Fprintf(os.Stdout, "- %s (not installed)\n", name)
	}

	for _, name := range result.NotFound {
		fmt.Fprintf(os.Stdout, "- %s (not found in catalog)\n", name)
	}

	// Print failures
	for _, name := range result.Failed {
		errMsg := result.Errors[name]
		fmt.Fprintf(os.Stderr, "✗ %s - failed: %s\n", name, errMsg)
	}

	if len(result.Failed) > 0 {
		return gatorpolicy.NewPartialSuccessError("upgrade incomplete: some policies failed to upgrade")
	}

	// Print summary
	if len(result.Upgraded) == 0 && len(result.Failed) == 0 {
		if len(result.NotFound) > 0 {
			fmt.Fprintf(os.Stdout, "\nNo upgrades available. %d policies not found in catalog.\n", len(result.NotFound))
		} else {
			fmt.Fprintln(os.Stdout, "\nAll policies are up to date.")
		}
	}

	return nil
}
