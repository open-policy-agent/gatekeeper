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
	installBundle            string
	installEnforcementAction string
	installVersion           string
	installDryRun            bool
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

# Install at a specific version
gator policy install k8srequiredlabels@v1.2.0

# Install a bundle (templates + constraints)
gator policy install --bundle pod-security-baseline

# Install bundle with warn enforcement
gator policy install --bundle pod-security-baseline --enforcement-action=warn

# Preview changes without applying
gator policy install --bundle pod-security-baseline --dry-run`,
		RunE: runInstall,
	}

	cmd.Flags().StringVar(&installBundle, "bundle", "", "Install a policy bundle (e.g., pod-security-baseline, pod-security-restricted)")
	cmd.Flags().StringVar(&installEnforcementAction, "enforcement-action", "", "Override enforcement action (deny, warn, dryrun)")
	cmd.Flags().StringVar(&installVersion, "version", "", "Install specific version")
	cmd.Flags().BoolVar(&installDryRun, "dry-run", false, "Preview changes without applying")

	return cmd
}

func runInstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()

	// Validate arguments
	if installBundle == "" && len(args) == 0 {
		return fmt.Errorf("specify policy name(s) or use --bundle to install a bundle")
	}
	// Note: --bundle with positional policies IS allowed per design.
	// Bundle is processed first, then individual policies are added (template-only).

	// Validate enforcement action
	if installEnforcementAction != "" {
		action := strings.ToLower(installEnforcementAction)
		if action != "deny" && action != "warn" && action != "dryrun" {
			return fmt.Errorf("invalid enforcement action: %s (must be deny, warn, or dryrun)", installEnforcementAction)
		}
		// Warn if enforcement action specified without bundle (template-only installs don't have constraints)
		if installBundle == "" {
			fmt.Fprintln(os.Stderr, "Warning: --enforcement-action is ignored for template-only installs (no bundle specified)")
		}
	}

	// Parse policy names and handle version suffix
	var policyNames []string
	policyVersions := make(map[string]string)
	for _, arg := range args {
		name := arg
		version := ""
		if idx := strings.Index(arg, "@"); idx != -1 {
			name = arg[:idx]
			version = arg[idx+1:]
		}
		policyNames = append(policyNames, name)
		if version != "" {
			policyVersions[name] = version
		}
	}

	// Validate mutual exclusivity: policy@vX and --version
	if installVersion != "" && len(policyVersions) > 0 {
		return fmt.Errorf("cannot use --version with policy@version syntax; use one or the other")
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
		PolicyVersions:    policyVersions,
		Bundle:            installBundle,
		EnforcementAction: installEnforcementAction,
		Version:           installVersion,
		DryRun:            installDryRun,
	}

	// Print header
	if installBundle != "" {
		bundlePolicies, err := cat.ResolveBundlePolicies(installBundle)
		if err != nil {
			return err
		}
		if installDryRun {
			fmt.Fprintf(os.Stdout, "==> Would install %s bundle (%d policies):\n", installBundle, len(bundlePolicies))
		} else {
			fmt.Fprintf(os.Stdout, "Installing %s bundle (%d policies)...\n", installBundle, len(bundlePolicies))
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
			len(result.Installed), len(result.Installed)+len(result.Failed))
		fmt.Fprintf(os.Stderr, "\nError: %s\n", msg)
		fmt.Fprintln(os.Stderr, "Re-run command to continue (already installed will be skipped).")
		return gatorpolicy.NewPartialSuccessError(msg)
	}

	// Print summary for bundles
	if installBundle != "" && !installDryRun {
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
