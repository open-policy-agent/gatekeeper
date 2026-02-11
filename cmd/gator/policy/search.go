package policy

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/output"
	"github.com/spf13/cobra"
)

var (
	searchCategory string
	searchBundle   string
	searchOutput   string
)

func newSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search available policies in the catalog",
		Long:  "Search for policies by name, description, or category.",
		Example: `# Search for label-related policies
gator policy search labels

# Search with category filter
gator policy search security --category=pod-security

# Search within a bundle
gator policy search container --bundle=pod-security-baseline

# Output as JSON
gator policy search labels --output=json`,
		Args: cobra.ExactArgs(1),
		RunE: runSearch,
	}

	cmd.Flags().StringVar(&searchCategory, "category", "", "Filter by category (e.g., general, pod-security)")
	cmd.Flags().StringVar(&searchBundle, "bundle", "", "Filter to policies within a bundle")
	cmd.Flags().StringVarP(&searchOutput, "output", "o", "table", "Output format: table, json")

	return cmd
}

func runSearch(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	query := strings.ToLower(args[0])

	// Load catalog from cache
	cache, err := catalog.NewCache()
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}

	cat, err := cache.LoadCatalog()
	if err != nil {
		// If no cache, try to fetch
		ctx := context.Background()
		fetcher := catalog.NewHTTPFetcher(catalog.DefaultTimeout)
		cat, err = catalog.LoadCatalog(ctx, fetcher, catalog.GetCatalogURL())
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nRun 'gator policy update' to refresh the catalog.")
			return fmt.Errorf("loading catalog: %w", err)
		}
	}

	// Build bundle filter set if --bundle is specified
	var bundleFilter map[string]bool
	if searchBundle != "" {
		bundlePolicies, err := cat.ResolveBundlePolicies(searchBundle)
		if err != nil {
			return err
		}
		bundleFilter = make(map[string]bool, len(bundlePolicies))
		for _, name := range bundlePolicies {
			bundleFilter[name] = true
		}
	}

	// Search policies
	var results []output.SearchResult
	for i := range cat.Policies {
		policy := &cat.Policies[i]
		// Filter by bundle if specified
		if bundleFilter != nil && !bundleFilter[policy.Name] {
			continue
		}

		// Filter by category if specified
		if searchCategory != "" && !strings.EqualFold(policy.Category, searchCategory) {
			continue
		}

		// Search in name and description
		nameLower := strings.ToLower(policy.Name)
		descLower := strings.ToLower(policy.Description)

		if strings.Contains(nameLower, query) || strings.Contains(descLower, query) {
			results = append(results, output.SearchResult{
				Name:        policy.Name,
				Version:     policy.Version,
				Category:    policy.Category,
				Description: policy.Description,
			})
		}
	}

	// Output results
	printer, err := output.NewPrinter(output.Format(searchOutput))
	if err != nil {
		return err
	}
	return printer.PrintSearchResults(os.Stdout, results)
}
