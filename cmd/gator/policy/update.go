package policy

import (
	"errors"
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/output"
	"github.com/spf13/cobra"
)

var (
	updateInsecure bool
	updateOutput   string
)

func newUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Refresh the policy catalog",
		Long:  "Download the latest policy catalog from gatekeeper-library.",
		Example: `# Update the catalog
gator policy update`,
		Args: cobra.NoArgs,
		RunE: runUpdate,
	}

	cmd.Flags().BoolVar(&updateInsecure, "insecure", false, "Allow plain HTTP catalog URLs (not recommended)")
	cmd.Flags().StringVarP(&updateOutput, "output", "o", "", "Output format: table (default) or json")

	return cmd
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Create printer
	printer, err := output.NewPrinter(output.Format(updateOutput))
	if err != nil {
		return err
	}

	catalogURL := catalog.GetCatalogURL()
	// Progress message to stderr so it doesn't pollute structured output
	fmt.Fprintf(os.Stderr, "Fetching catalog from %s...\n", catalogURL)

	// Fetch catalog
	fetcher := catalog.NewHTTPFetcher(catalog.DefaultTimeout)
	if updateInsecure {
		fetcher.SetInsecure(true)
		fmt.Fprintln(os.Stderr, "Warning: --insecure flag set, allowing plain HTTP (not recommended for production)")
	}
	data, err := fetcher.Fetch(ctx, catalogURL)
	if err != nil {
		// Check for insecure HTTP error and provide helpful message
		if errors.Is(err, catalog.ErrInsecureHTTP) {
			return fmt.Errorf("%w; use --insecure to override (not recommended)", err)
		}
		return fmt.Errorf("fetching catalog: %w", err)
	}

	// Parse to validate
	cat, err := catalog.ParseCatalog(data)
	if err != nil {
		return fmt.Errorf("parsing catalog: %w", err)
	}

	// Save to cache
	cache, err := catalog.NewCache()
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	if err := cache.SaveCatalog(data, catalogURL); err != nil {
		return fmt.Errorf("saving catalog to cache: %w", err)
	}

	// Build update result
	result := &output.UpdateResult{
		CatalogVersion: cat.Metadata.Version,
		PolicyCount:    len(cat.Policies),
		BundleCount:    len(cat.Bundles),
	}

	// Check for upgradable policies if cluster is accessible
	k8sClient, err := client.NewK8sClient()
	if err == nil {
		installed, err := k8sClient.ListManagedTemplates(ctx)
		if err == nil && len(installed) > 0 {
			upgradable := client.GetUpgradablePolicies(installed, cat)
			for _, change := range upgradable {
				result.Upgradable = append(result.Upgradable, output.UpgradeEntry{
					Name:        change.Name,
					FromVersion: change.FromVersion,
					ToVersion:   change.ToVersion,
				})
			}
		}
	}

	return printer.PrintUpdateResult(os.Stdout, result)
}
