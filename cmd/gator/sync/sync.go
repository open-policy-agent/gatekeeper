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
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Usage: gator sync test")
	},
}

func init() {
	Cmd.AddCommand(commands...)
}
