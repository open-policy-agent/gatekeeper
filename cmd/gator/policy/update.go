package policy

import (
	"context"
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/spf13/cobra"
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

	return cmd
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	ctx := context.Background()

	catalogURL := catalog.GetCatalogURL()
	fmt.Fprintf(os.Stdout, "Fetching catalog from %s...\n", catalogURL)

	// Fetch catalog
	fetcher := catalog.NewHTTPFetcher(catalog.DefaultTimeout)
	data, err := fetcher.Fetch(ctx, catalogURL)
	if err != nil {
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

	if err := cache.SaveCatalog(data); err != nil {
		return fmt.Errorf("saving catalog to cache: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Updated catalog to version %s (%d policies, %d bundles)\n",
		cat.Metadata.Version, len(cat.Policies), len(cat.Bundles))

	// Check for upgradable policies if cluster is accessible
	k8sClient, err := client.NewK8sClient()
	if err == nil {
		installed, err := k8sClient.ListManagedTemplates(ctx)
		if err == nil && len(installed) > 0 {
			upgradable := client.GetUpgradablePolicies(installed, cat)
			if len(upgradable) > 0 {
				fmt.Fprintf(os.Stdout, "\n%d policies have updates available:\n", len(upgradable))
				for _, change := range upgradable {
					fmt.Fprintf(os.Stdout, "  %s  %s → %s\n", change.Name, change.FromVersion, change.ToVersion)
				}
				fmt.Fprintln(os.Stdout, "\nRun 'gator policy upgrade --all' to upgrade.")
			}
		}
	}

	return nil
}
