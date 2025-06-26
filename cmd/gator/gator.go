package main

import (
	"os"

	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/expand"
	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/sync"
	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/test"
	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/verify"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/version"
	"github.com/spf13/cobra"
	k8sVersion "sigs.k8s.io/release-utils/version"
)

var commands = []*cobra.Command{
	verify.Cmd,
	test.Cmd,
	expand.Cmd,
	sync.Cmd,
	k8sVersion.WithFont("dotmatrix"),
}

func init() {
	rootCmd.AddCommand(commands...)
	rootCmd.Version = version.GetUserAgent("gator")
}

var rootCmd = &cobra.Command{
	Use:   "gator subcommand",
	Short: "gator is a suite of authorship tools for Gatekeeper",
	Long: `
Gator is a suite of authorship tools designed to improve the developer experience when working with Gatekeeper.
It supports:
  - Validating Kubernetes manifests against constraints
  - Expanding Rego-based constraints for debugging
  - Running policy tests locally
  - Verifying ConstraintTemplates and Constraints before deploying

Use it to catch issues early, test policies offline, and ensure compliance.`,
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
