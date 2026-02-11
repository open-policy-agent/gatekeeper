package policy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	gatorpolicy "github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/catalog"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	installBundles           []string
	installEnforcementAction string
	installDryRun            bool
	installOutput            string
)

func newInstallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <policy...>",
		Short: "Install one or more policies",
		Long: `Install policies from the gatekeeper-library into the cluster.

Individual policies install only the ConstraintTemplate.
Bundles install both ConstraintTemplates and pre-configured Constraints.`,
		Example: `# Install a single policy (template only)
gator policy install k8srequiredlabels

# Install multiple policies
gator policy install k8srequiredlabels k8scontainerlimits

# Install a bundle (templates + constraints)
gator policy install --bundle pod-security-baseline

# Install multiple bundles
gator policy install --bundle pod-security-baseline --bundle pod-security-restricted

# Install bundle with warn enforcement
gator policy install --bundle pod-security-baseline --enforcement-action=warn

# Preview changes without applying
gator policy install --bundle pod-security-baseline --dry-run

# Output results as JSON
gator policy install --bundle pod-security-baseline -o json`,
		RunE: runInstall,
	}

	cmd.Flags().StringSliceVar(&installBundles, "bundle", nil, "Install a policy bundle (may be specified multiple times)")
	cmd.Flags().StringVar(&installEnforcementAction, "enforcement-action", "", "Override enforcement action (deny, warn, dryrun). Note: 'scoped' is not supported in this release.")
	cmd.Flags().BoolVar(&installDryRun, "dry-run", false, "Preview changes without applying (requires cluster access — uses server-side dry run)")
	cmd.Flags().StringVarP(&installOutput, "output", "o", "", "Output format: table (default) or json")

	return cmd
}

func runInstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Validate arguments
	if len(installBundles) == 0 && len(args) == 0 {
		return fmt.Errorf("specify policy name(s) or use --bundle to install a bundle")
	}
	// Note: --bundle with positional policies IS allowed per design.
	// Bundle is processed first, then individual policies are added (template-only).

	// Validate enforcement action
	if installEnforcementAction != "" {
		action := strings.ToLower(installEnforcementAction)
		if action == "scoped" {
			return fmt.Errorf("'scoped' enforcement action is not supported in this release")
		}
		if action != "deny" && action != "warn" && action != "dryrun" {
			return fmt.Errorf("invalid enforcement action: %s (must be deny, warn, or dryrun)", installEnforcementAction)
		}
		// Warn if enforcement action specified without bundle (template-only installs don't have constraints)
		if len(installBundles) == 0 {
			fmt.Fprintln(os.Stderr, "Warning: --enforcement-action is ignored for template-only installs (no bundle specified)")
		}
	}

	// Validate output format early
	if installOutput != "" && installOutput != "table" && installOutput != "json" {
		return fmt.Errorf("invalid output format: %s (must be table or json)", installOutput)
	}

	// Parse policy names
	policyNames := args

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

	// Create fetcher for templates/constraints with the catalog URL as base
	fetcher := catalog.NewHTTPFetcherWithBaseURL(catalog.DefaultTimeout, catalog.GetCatalogURL())

	// Create Kubernetes client (unless dry-run)
	var k8sClient client.Client
	if !installDryRun {
		k8sClient, err = client.NewK8sClient()
		if err != nil {
			return fmt.Errorf("creating Kubernetes client: %w", err)
		}
	} else {
		k8sClient = &dryRunClient{}
	}

	// Build install options
	opts := &client.InstallOptions{
		Policies:          policyNames,
		Bundles:           installBundles,
		EnforcementAction: installEnforcementAction,
		DryRun:            installDryRun,
	}

	// Print header
	if len(installBundles) > 0 {
		for _, b := range installBundles {
			bundlePolicies, err := cat.ResolveBundlePolicies(b)
			if err != nil {
				return err
			}
			if installDryRun {
				fmt.Fprintf(os.Stdout, "==> Would install %s bundle (%d policies):\n", b, len(bundlePolicies))
			} else {
				fmt.Fprintf(os.Stdout, "Installing %s bundle (%d policies)...\n", b, len(bundlePolicies))
			}
		}
	} else if installDryRun {
		fmt.Fprintf(os.Stdout, "==> Would install %d policies:\n", len(policyNames))
	}

	// Perform installation
	result, err := client.Install(ctx, k8sClient, fetcher, cat, opts)
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
	for _, name := range result.Installed {
		policy := cat.GetPolicy(name)
		version := ""
		if policy != nil {
			version = policy.Version
		}
		if installDryRun {
			fmt.Fprintf(os.Stdout, "%s %s\n", name, version)
		} else {
			fmt.Fprintf(os.Stdout, "✓ %s (%s) installed\n", name, version)
		}
	}

	for _, name := range result.Skipped {
		fmt.Fprintf(os.Stdout, "- %s (already installed at same version)\n", name)
	}

	// Print failures
	if len(result.Failed) > 0 {
		for _, name := range result.Failed {
			errMsg := result.Errors[name]
			fmt.Fprintf(os.Stderr, "✗ %s - failed: %s\n", name, errMsg)
		}

		// Check if we have a conflict error
		if result.ConflictErr != nil {
			return gatorpolicy.NewConflictError(fmt.Sprintf("installation incomplete: %s", result.ConflictErr.Error()))
		}

		msg := fmt.Sprintf("installation incomplete: %d of %d policies installed",
			len(result.Installed), result.TotalRequested)
		fmt.Fprintln(os.Stderr, "\nRe-run command to continue (already installed will be skipped).")
		return gatorpolicy.NewPartialSuccessError(msg)
	}

	// Print summary for bundles
	if len(installBundles) > 0 && !installDryRun {
		fmt.Fprintf(os.Stdout, "\n✓ Installed %d templates, %d constraints\n",
			result.TemplatesInstalled, result.ConstraintsInstalled)
	}

	return nil
}

// dryRunClient is a no-op client for dry-run mode.
type dryRunClient struct{}

func (c *dryRunClient) GatekeeperInstalled(_ context.Context) (bool, error) {
	return true, nil
}

func (c *dryRunClient) ListManagedTemplates(_ context.Context) ([]client.InstalledPolicy, error) {
	return nil, nil
}

func (c *dryRunClient) GetTemplate(_ context.Context, _ string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (c *dryRunClient) InstallTemplate(_ context.Context, _ *unstructured.Unstructured) error {
	return nil
}

func (c *dryRunClient) InstallConstraint(_ context.Context, _ *unstructured.Unstructured) error {
	return nil
}

func (c *dryRunClient) GetConstraint(_ context.Context, _ schema.GroupVersionResource, _ string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (c *dryRunClient) DeleteTemplate(_ context.Context, _ string) error {
	return nil
}

func (c *dryRunClient) DeleteConstraint(_ context.Context, _ schema.GroupVersionResource, _ string) error {
	return nil
}

func (c *dryRunClient) WaitForTemplateReady(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (c *dryRunClient) WaitForConstraintCRD(_ context.Context, _ string, _ time.Duration) error {
	return nil
}
