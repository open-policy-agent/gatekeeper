package policy

import (
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
)

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
