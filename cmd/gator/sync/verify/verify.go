package verify

import (
	"fmt"
	"os"
	"strings"

	cmdutils "github.com/open-policy-agent/gatekeeper/v3/cmd/gator/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/sync/verify"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify that the provided SyncSet(s) and/or Config contain the GVKs required by the input templates.",
	Run:   run,
}

var (
	flagFilenames     []string
	flagImages        []string
	flagSupportedGVKs verify.SupportedGVKs
)

const (
	flagNameFilename      = "filename"
	flagNameImage         = "image"
	flagNameSupportedGVKs = "supported-gvks"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.")
	Cmd.Flags().StringArrayVarP(&flagImages, flagNameImage, "i", []string{}, "a URL to an OCI image containing policies. Can be specified multiple times.")
	Cmd.Flags().VarP(&flagSupportedGVKs, flagNameSupportedGVKs, "s", "a json string listing the GVKs supported by the cluster as a nested array of groups, containing supported versions, each of which contains supported kinds. See https://open-policy-agent.github.io/gatekeeper/website/docs/gator#the-gator-sync-verify-subcommand for an example.")
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := reader.ReadSources(flagFilenames, flagImages, "")
	if err != nil {
		cmdutils.ErrFatalf("reading: %v", err)
	}
	if len(unstrucs) == 0 {
		cmdutils.ErrFatalf("no input data identified")
	}

	missingRequirements, templateErrors, err := verify.Verify(unstrucs, flagSupportedGVKs)
	if err != nil {
		cmdutils.ErrFatalf("verifying: %v", err)
	}

	if len(missingRequirements) > 0 {
		cmdutils.ErrFatalf("The following requirements were not met: \n%v", resultsToString(missingRequirements))
	}

	if len(templateErrors) > 0 {
		cmdutils.ErrFatalf("Encountered errors parsing the following templates: \n%v", resultsToString(templateErrors))
	}

	fmt.Println("All template requirements met.")
	os.Exit(0)
}

func resultsToString[T any](results map[string]T) string {
	var sb strings.Builder
	for template, vals := range results {
		sb.WriteString(fmt.Sprintf("%s:\n%v\n", template, vals))
	}
	return sb.String()
}
