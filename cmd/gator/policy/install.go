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
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/output"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	installBundles           []string
	installEnforcementAction string
	installDryRun            bool
	installForce             bool
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

# Install even if the cluster Kubernetes version is outside a policy's supported range
gator policy install k8srequiredlabels --force

# Output as JSON for scripting
gator policy install --bundle pod-security-baseline --dry-run -o json`,
		RunE: runInstall,
	}

	cmd.Flags().StringSliceVar(&installBundles, "bundle", nil, "Install a policy bundle (may be specified multiple times)")
	cmd.Flags().StringVar(&installEnforcementAction, "enforcement-action", "", "Override enforcement action (deny, warn, dryrun). Note: 'scoped' is not supported in this release.")
	cmd.Flags().BoolVar(&installDryRun, "dry-run", false, "Preview changes without applying (requires cluster access to check Kubernetes version compatibility)")
	cmd.Flags().BoolVar(&installForce, "force", false, "Install even if the cluster Kubernetes version is outside a policy's supported range")
	cmd.Flags().StringVarP(&installOutput, "output", "o", "table", "Output format: table, json")

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

	// Create printer
	printer, err := output.NewPrinter(output.Format(installOutput))
	if err != nil {
		return err
	}

	// Parse policy names
	policyNames := args

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

	// Create fetcher for templates/constraints with the cached catalog source URL as base
	fetcher := catalog.NewHTTPFetcherWithBaseURL(catalog.DefaultTimeout, catalogSourceURL)

	// Create Kubernetes client. Like upgrade, install --dry-run contacts the
	// cluster when it can so the preview applies the same Kubernetes-version
	// compatibility gate a real install would.
	var k8sClient client.Client
	k8sClient, err = client.NewK8sClient()
	if err != nil {
		if !installDryRun {
			return fmt.Errorf("creating Kubernetes client: %w", err)
		}
		// A dry-run only needs the cluster to gate policies that declare a Kubernetes version range. Fall back to a stub so previewing policies
		// without version bounds (or with --force) still works offline; if the gate is actually active, the stub surfaces this connection error
		// instead of silently skipping the check.
		k8sClient = &dryRunFallbackClient{
			err: fmt.Errorf("creating Kubernetes client for the version-compatibility check (use --force to skip it): %w", err),
		}
	}

	// Build install options
	opts := &client.InstallOptions{
		Policies:          policyNames,
		Bundles:           installBundles,
		EnforcementAction: installEnforcementAction,
		DryRun:            installDryRun,
		Force:             installForce,
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

	// Build output result
	outResult := &output.InstallResult{
		TemplatesInstalled:   result.TemplatesInstalled,
		ConstraintsInstalled: result.ConstraintsInstalled,
		DryRun:               installDryRun,
	}

	for _, name := range result.Installed {
		version := ""
		if policy := cat.GetPolicy(name); policy != nil {
			version = policy.Version
		}
		outResult.Installed = append(outResult.Installed, output.InstallEntry{
			Name:    name,
			Version: version,
		})
	}
	outResult.Skipped = result.Skipped

	outResult.Incompatible = result.Incompatible

	for _, name := range result.Failed {
		outResult.Failed = append(outResult.Failed, output.FailedEntry{
			Name:  name,
			Error: result.Errors[name],
		})
	}

	// Print results
	if printErr := printer.PrintInstallResult(os.Stdout, outResult); printErr != nil {
		return printErr
	}

	// Return appropriate error for non-success cases
	if len(result.Failed) > 0 {
		if result.ConflictErr != nil {
			return gatorpolicy.NewConflictError(fmt.Sprintf("installation incomplete: %s", result.ConflictErr.Error()))
		}

		msg := fmt.Sprintf("installation incomplete: %d of %d policies installed",
			len(result.Installed), result.TotalRequested)
		// When incompatible policies are also present, the Incompatible branch
		// below is unreachable, so fold its guidance into this message rather than
		// dropping the "--force" hint.
		if len(result.Incompatible) > 0 {
			msg += fmt.Sprintf(" (%d skipped: %s)", len(result.Incompatible), incompatibleGuidance)
		}
		fmt.Fprintln(os.Stderr, "\nRe-run command to continue (already installed will be skipped).")
		return gatorpolicy.NewPartialSuccessError(msg)
	}

	// Policies skipped as incompatible with the cluster's Kubernetes version were
	// explicitly requested but not installed, so signal partial success rather
	// than exiting 0 as if everything succeeded.
	if len(result.Incompatible) > 0 {
		msg := fmt.Sprintf("installation incomplete: %d of %d policies installed (%d skipped: %s)",
			len(result.Installed), result.TotalRequested, len(result.Incompatible), incompatibleGuidance)
		return gatorpolicy.NewPartialSuccessError(msg)
	}

	return nil
}

// dryRunFallbackClient stands in for a real client when one cannot be built for
// a dry-run (e.g. no reachable cluster or kubeconfig). A dry-run only ever calls
// ServerVersion, and only when the compatibility gate is active (a bounded
// policy without --force). Every method reports the client-creation failure so
// that any call fails loudly with a clear error instead of panicking or
// silently succeeding.
type dryRunFallbackClient struct {
	err error
}

func (c *dryRunFallbackClient) GatekeeperInstalled(context.Context) (bool, error) {
	return false, c.err
}

// ServerVersion reports the client-creation failure so the gate fails loudly for
// bounded policies rather than silently skipping the check.
func (c *dryRunFallbackClient) ServerVersion(context.Context) (string, error) {
	return "", c.err
}

func (c *dryRunFallbackClient) ListManagedTemplates(context.Context) ([]client.InstalledPolicy, error) {
	return nil, c.err
}

func (c *dryRunFallbackClient) GetTemplate(context.Context, string) (*unstructured.Unstructured, error) {
	return nil, c.err
}

func (c *dryRunFallbackClient) InstallTemplate(context.Context, *unstructured.Unstructured) error {
	return c.err
}

func (c *dryRunFallbackClient) InstallConstraint(context.Context, *unstructured.Unstructured) error {
	return c.err
}

func (c *dryRunFallbackClient) GetConstraint(context.Context, schema.GroupVersionResource, string) (*unstructured.Unstructured, error) {
	return nil, c.err
}

func (c *dryRunFallbackClient) DeleteTemplate(context.Context, string) error {
	return c.err
}

func (c *dryRunFallbackClient) DeleteConstraint(context.Context, schema.GroupVersionResource, string) error {
	return c.err
}

func (c *dryRunFallbackClient) WaitForTemplateReady(context.Context, string, time.Duration) error {
	return c.err
}

func (c *dryRunFallbackClient) WaitForConstraintCRD(context.Context, string, time.Duration) error {
	return c.err
}
