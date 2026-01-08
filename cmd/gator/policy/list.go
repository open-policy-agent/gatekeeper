package policy

import (
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/output"
	"github.com/spf13/cobra"
)

var listOutput string

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed policies",
		Long:  "List all Gatekeeper policies managed by gator that are installed in the cluster.",
		Example: `# List installed policies
gator policy list

# Output as JSON
gator policy list --output=json`,
		Args: cobra.NoArgs,
		RunE: runList,
	}

	cmd.Flags().StringVarP(&listOutput, "output", "o", "table", "Output format: table, json")

	return cmd
}

func runList(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Create Kubernetes client
	k8sClient, err := client.NewK8sClient()
	if err != nil {
		return fmt.Errorf("creating Kubernetes client: %w", err)
	}

	// Check if Gatekeeper is installed
	installed, err := k8sClient.GatekeeperInstalled(ctx)
	if err != nil {
		return fmt.Errorf("checking Gatekeeper installation: %w", err)
	}
	if !installed {
		return fmt.Errorf("gatekeeper CRDs not found in cluster")
	}

	// List managed policies
	policies, err := k8sClient.ListManagedTemplates(ctx)
	if err != nil {
		return fmt.Errorf("listing policies: %w", err)
	}

	// Convert to output format
	policyInfos := make([]output.PolicyInfo, len(policies))
	for i, p := range policies {
		policyInfos[i] = output.PolicyInfo{
			Name:        p.Name,
			Version:     p.Version,
			Bundle:      p.Bundle,
			InstalledAt: p.InstalledAt,
		}
	}

	// Check for available updates
	cache, err := catalog.NewCache()
	if err == nil {
		cat, err := cache.LoadCatalog()
		if err == nil {
			upgradable := client.GetUpgradableCount(policies, cat)
			if upgradable > 0 {
				fmt.Fprintf(os.Stderr, "Hint: %d policy(ies) have updates available. Run 'gator policy update' then 'gator policy upgrade --all'.\n\n", upgradable)
			}
		}
	}

	// Output results
	printer := output.NewPrinter(output.Format(listOutput))
	return printer.PrintPolicies(os.Stdout, policyInfos)
}
