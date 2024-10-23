package sync

import (
	"fmt"

	synctest "github.com/open-policy-agent/gatekeeper/v3/cmd/gator/sync/test"
	"github.com/spf13/cobra"
)

var commands = []*cobra.Command{
	synctest.Cmd,
}

var Cmd = &cobra.Command{
	Use:   "sync",
	Short: "Manage SyncSets and Config",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("Usage: gator sync test")
	},
}

func init() {
	Cmd.AddCommand(commands...)
}
