package syncverify

import (
	"github.com/spf13/cobra"
)

// Cmd represents the verify command
var Cmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify that the provided SyncSet(s) and/or Config contain the GVKs required by the input templates.",
	RunE:  runE,
}

var (
	flagFilenames         []string
	flagSyncDataFilenames []string
	flagDiscoveryResults  string
)

const (
	flagNameTemplateFilename = "template-filename"
	flagNameSyncDataFilename = "sync-data-filename"
	flagNameDiscoveryResults = "discovery-results"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameTemplateFilename, "f", []string{}, "a file or directory containing the Constraint Templates to verify SyncSets/Config against.  Can be specified multiple times.")
	Cmd.MarkFlagRequired(flagNameTemplateFilename)

	Cmd.Flags().StringArrayVarP(&flagSyncDataFilenames, flagNameSyncDataFilename, "s", []string{}, "a file or directory containing the SyncSet(s) and/or Config to verify.  Can be specified multiple times.")
	Cmd.MarkFlagRequired(flagNameSyncDataFilename)

	Cmd.Flags().StringVarP(&flagDiscoveryResults, flagDiscoveryResults, "d", "", "a json string listing the GVKs supported by the cluster as a nested array of groups, containing supported versions, containing supported kinds.")

}

func runE(cmd *cobra.Command, args []string) error {
	return nil
}
