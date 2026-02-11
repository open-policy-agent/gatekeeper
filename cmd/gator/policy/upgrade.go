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
	upgradeBundles           []string
	upgradeEnforcementAction string
	upgradeDryRun            bool
	upgradeOutput            string
)

func newUpgradeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade [policy...]",
		Short: "Upgrade installed policies to latest versions",
		Long: `Upgrade installed policies to their latest versions from the catalog.

Requires specifying policy name(s), --bundle, or --all flag.`,
		Example: `# Upgrade a specific policy
gator policy upgrade k8srequiredlabels

# Upgrade all policies in a bundle
gator policy upgrade --bundle pod-security-baseline

# Upgrade all installed policies
gator policy upgrade --all

# Preview changes without applying
gator policy upgrade --all --dry-run

# Output results as JSON
gator policy upgrade --all -o json`,
		RunE: runUpgrade,
	}

	cmd.Flags().BoolVar(&upgradeAll, "all", false, "Upgrade all installed policies")
	cmd.Flags().StringSliceVar(&upgradeBundles, "bundle", nil, "Upgrade all policies in a bundle (may be specified multiple times)")
	cmd.Flags().StringVar(&upgradeEnforcementAction, "enforcement-action", "", "Override enforcement action (deny, warn, dryrun)")
	cmd.Flags().BoolVar(&upgradeDryRun, "dry-run", false, "Preview changes without applying (requires cluster access \u2014 uses server-side dry run)")
	cmd.Flags().StringVarP(&upgradeOutput, "output", "o", "", "Output format: table (default) or json")

	return cmd
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Validate arguments
	if !upgradeAll && len(upgradeBundles) == 0 && len(args) == 0 {
		return fmt.Errorf("specify policy name(s), use --bundle, or use --all to upgrade all policies")
	}

	// Validate enforcement action
	if upgradeEnforcementAction != "" {
		action := strings.ToLower(upgradeEnforcementAction)
		if action != "deny" && action != "warn" && action != "dryrun" {
			return fmt.Errorf("invalid enforcement action: %s (must be deny, warn, or dryrun)", upgradeEnforcementAction)
		}
	}

	// Validate output format early
	if upgradeOutput != "" && upgradeOutput != "table" && upgradeOutput != "json" {
		return fmt.Errorf("invalid output format: %s (must be table or json)", upgradeOutput)
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

	// Resolve bundle policies if --bundle is specified
	policyNames := args
	if len(upgradeBundles) > 0 {
		seen := make(map[string]bool)
		for _, name := range policyNames {
			seen[name] = true
		}
		for _, b := range upgradeBundles {
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

	// Build upgrade options
	opts := client.UpgradeOptions{
		Policies:          policyNames,
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
