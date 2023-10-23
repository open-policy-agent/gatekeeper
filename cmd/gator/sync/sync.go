package sync

import (
	"fmt"

	syncverify "github.com/open-policy-agent/gatekeeper/v3/cmd/gator/sync/verify"
	"github.com/spf13/cobra"
)

var commands = []*cobra.Command{
	syncverify.Cmd,
}

var Cmd = &cobra.Command{
	Use:   "sync",
	Short: "Manage SyncSets and Config",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: gator sync verify")
	},
}

func init() {
	Cmd.AddCommand(commands...)
}
