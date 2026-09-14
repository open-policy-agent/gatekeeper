package policy

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

const (
	examples = `# Search for policies
gator policy search labels

# List installed policies
gator policy list

# Install a policy
gator policy install k8srequiredlabels

# Install a bundle with warn enforcement
gator policy install --bundle pod-security-baseline --enforcement-action=warn

# Update the policy catalog
gator policy update

# Upgrade all policies
gator policy upgrade --all

# Uninstall a policy
gator policy uninstall k8srequiredlabels

# Generate a catalog from gatekeeper-library
gator policy generate-catalog --library-path=/path/to/gatekeeper-library`

	// incompatibleGuidance is the shared hint appended to install/upgrade
	// result messages when policies were skipped for being incompatible with
	// the cluster's Kubernetes version, so install and upgrade can't drift
	// apart in wording.
	incompatibleGuidance = "incompatible Kubernetes version, use --force to override"

	// unknownGuidance is the hint appended to install result messages when a
	// policy's Kubernetes-version compatibility could not be determined
	// offline during --dry-run.
	unknownGuidance = "compatibility unknown in offline dry-run preview, re-run without --dry-run (or use --force) to determine compatibility"

	// versionLookupTimeout bounds the best-effort cluster Kubernetes version
	// lookup that list/update use to decide whether to advertise an upgrade.
	// It's a single lightweight GET, and both callers already fall back to an
	// ungated, catalog-version-only comparison when it fails, so a short bound
	// keeps a stalled cluster from blocking the command instead of degrading
	// gracefully.
	versionLookupTimeout = 5 * time.Second
)

// incompatibleSkipSuffix renders the "(N skipped: <guidance>)" fragment appended
// to install/upgrade partial-success messages when n policies were skipped for
// being incompatible with the cluster's Kubernetes version.
func incompatibleSkipSuffix(n int) string {
	return fmt.Sprintf(" (%d skipped: %s)", n, incompatibleGuidance)
}

// unknownSkipSuffix renders the "(N unknown: <guidance>)" fragment appended to
// install partial-success messages when n policies' compatibility could not
// be determined during an offline dry-run preview.
func unknownSkipSuffix(n int) string {
	return fmt.Sprintf(" (%d unknown: %s)", n, unknownGuidance)
}

// Cmd is the gator policy subcommand.
var Cmd = &cobra.Command{
	Use:     "policy",
	Short:   "Manage Gatekeeper policies from the policy library",
	Long:    "Install, upgrade, and manage Gatekeeper policies from the official gatekeeper-library.",
	Example: examples,
}

func init() {
	Cmd.AddCommand(
		newSearchCommand(),
		newListCommand(),
		newInstallCommand(),
		newUninstallCommand(),
		newUpdateCommand(),
		newUpgradeCommand(),
		newGenerateCatalogCommand(),
	)
}
