package main

import (
	"os"

	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/expand"
	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/policy"
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
	policy.Cmd,
	k8sVersion.WithFont("alligator2"),
}

func init() {
	rootCmd.AddCommand(commands...)
	rootCmd.Version = version.GetUserAgent("gator")
}

var rootCmd = &cobra.Command{
	Use:   "gator subcommand",
	Short: "gator is a suite of authorship tools for Gatekeeper",
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
