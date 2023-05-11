package main

import (
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/expand"
	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/test"
	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/verify"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/version"
	"github.com/spf13/cobra"
	k8sVersion "sigs.k8s.io/release-utils/version"
)

const state = "beta"

var (
	frameworksVersion string
	opaVersion        string
)

var commands = []*cobra.Command{
	verify.Cmd,
	test.Cmd,
	expand.Cmd,
	k8sVersion.WithFont("alligator2"),
}

func init() {
	rootCmd.AddCommand(commands...)
}

var rootCmd = &cobra.Command{
	Use:     "gator subcommand",
	Short:   "gator is a suite of authorship tools for Gatekeeper",
	Version: fmt.Sprintf("%s (Feature State: %s), OPA version: %s, Framework version: %s", version.Version, state, opaVersion, frameworksVersion),
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
