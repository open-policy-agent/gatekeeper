package verify

import (
	"bytes"
	"fmt"
	"os"

	cmdutils "github.com/open-policy-agent/gatekeeper/v3/cmd/gator/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/sync/verify"
	"github.com/spf13/cobra"
)

// Cmd represents the verify command
var Cmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify that the provided SyncSet(s) and/or Config contain the GVKs required by the input templates.",
	Run:   run,
}

var (
	flagFilenames        []string
	flagImages           []string
	flagDiscoveryResults string
)

const (
	flagNameFilename         = "filename"
	flagNameImage            = "image"
	flagNameDiscoveryResults = "discovery-results"
)

func init() {

	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.")
	Cmd.Flags().StringArrayVarP(&flagImages, flagNameImage, "i", []string{}, "a URL to an OCI image containing policies. Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagDiscoveryResults, flagDiscoveryResults, "d", "", "a json string listing the GVKs supported by the cluster as a nested array of groups, containing supported versions, containing supported kinds.")

}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := reader.ReadSources(flagFilenames, flagImages, "")
	if err != nil {
		cmdutils.ErrFatalf("reading: %v", err)
	}
	if len(unstrucs) == 0 {
		cmdutils.ErrFatalf("no input data identified")
	}

	missingRequirements, err := verify.Verify(unstrucs, flagDiscoveryResults)

	if err != nil {
		cmdutils.ErrFatalf("verifying: %v", err)
	}

	if len(missingRequirements) > 0 {
		cmdutils.ErrFatalf("The following requirements were not met: \n%v", resultsToString(missingRequirements))
	}

	os.Exit(0)
}

func resultsToString(results map[string][]int) string {
	var buf bytes.Buffer
	for template, reqs := range results {
		buf.WriteString(fmt.Sprintf("%s: %v\n", template, reqs))
	}
	return buf.String()
}
