package policy

import (
	"errors"
	"fmt"
	"os"
	"strings"

	gatorpolicy "github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/output"
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

# Output as JSON for scripting
gator policy upgrade --all -o json`,
		RunE: runUpgrade,
	}

	cmd.Flags().BoolVar(&upgradeAll, "all", false, "Upgrade all installed policies")
	cmd.Flags().StringSliceVar(&upgradeBundles, "bundle", nil, "Upgrade all policies in a bundle (may be specified multiple times)")
	cmd.Flags().StringVar(&upgradeEnforcementAction, "enforcement-action", "", "Override enforcement action (deny, warn, dryrun)")
	cmd.Flags().BoolVar(&upgradeDryRun, "dry-run", false, "Preview changes without applying (requires cluster access to check current state)")
	cmd.Flags().StringVarP(&upgradeOutput, "output", "o", "table", "Output format: table, json")

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

	// Create printer
	printer, err := output.NewPrinter(output.Format(upgradeOutput))
	if err != nil {
		return err
	}

	// Load catalog
	cache, err := catalog.NewCache()
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	cat, catalogSourceURL, err := cache.LoadCatalogWithSource()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nRun 'gator policy update' to refresh the catalog.")
		return fmt.Errorf("loading catalog: %w", err)
	}

	// Create Kubernetes client
	k8sClient, err := client.NewK8sClient()
	if err != nil {
		return fmt.Errorf("creating Kubernetes client: %w", err)
	}

	// Create fetcher for templates/constraints with the cached catalog source URL as base
	fetcher := catalog.NewHTTPFetcherWithBaseURL(catalog.DefaultTimeout, catalogSourceURL)

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

	// Build output result
	outResult := &output.UpgradeResult{
		AlreadyCurrent: result.AlreadyCurrent,
		NotInstalled:   result.NotInstalled,
		NotFound:       result.NotFound,
		DryRun:         upgradeDryRun,
	}

	for _, change := range result.Upgraded {
		outResult.Upgraded = append(outResult.Upgraded, output.UpgradeEntry{
			Name:        change.Name,
			FromVersion: change.FromVersion,
			ToVersion:   change.ToVersion,
		})
	}

	for _, name := range result.Failed {
		outResult.Failed = append(outResult.Failed, output.FailedEntry{
			Name:  name,
			Error: result.Errors[name],
		})
	}

	// Print results
	if printErr := printer.PrintUpgradeResult(os.Stdout, outResult); printErr != nil {
		return printErr
	}

	// Return appropriate error for non-success cases
	if len(result.Failed) > 0 {
		return gatorpolicy.NewPartialSuccessError("upgrade incomplete: some policies failed to upgrade")
	}

	return nil
}
