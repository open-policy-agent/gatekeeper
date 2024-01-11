package test

import (
	"fmt"
	"os"
	"strings"

	cmdutils "github.com/open-policy-agent/gatekeeper/v3/cmd/gator/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/sync/test"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "test",
	Short: "Test that the provided SyncSet(s) and/or Config contain the GVKs required by the input templates.",
	Run:   run,
}

var (
	flagFilenames       []string
	flagImages          []string
	flagOmitGVKManifest bool
)

const (
	flagNameFilename = "filename"
	flagNameImage    = "image"
	flagNameForce    = "force-omit-gvk-manifest"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.")
	Cmd.Flags().StringArrayVarP(&flagImages, flagNameImage, "i", []string{}, "a URL to an OCI image containing policies. Can be specified multiple times.")
	Cmd.Flags().BoolVarP(&flagOmitGVKManifest, flagNameForce, "o", false, "Do not require a GVK manifest; if one is not provided, assume all GVKs listed in the requirements "+
		"and configs are supported by the cluster under test. If this assumption isn't true, the given config may cause errors or templates may not be enforced correctly even after passing this test.")
}

func run(_ *cobra.Command, _ []string) {
	unstrucs, err := reader.ReadSources(flagFilenames, flagImages, "")
	if err != nil {
		cmdutils.ErrFatalf("reading: %v", err)
	}
	if len(unstrucs) == 0 {
		cmdutils.ErrFatalf("no input data identified")
	}

	missingRequirements, templateErrors, err := test.Test(unstrucs, flagOmitGVKManifest)
	if err != nil {
		cmdutils.ErrFatalf("checking: %v", err)
	}

	if len(missingRequirements) > 0 {
		cmdutils.ErrFatalf("the following requirements were not met: \n%v", resultsToString(missingRequirements))
	}

	if len(templateErrors) > 0 {
		cmdutils.ErrFatalf("encountered errors parsing the following templates: \n%v", resultsToString(templateErrors))
	}

	os.Exit(0)
}

func resultsToString[T any](results map[string]T) string {
	var sb strings.Builder
	for template, vals := range results {
		sb.WriteString(fmt.Sprintf("%s:\n%v\n", template, vals))
	}
	return sb.String()
}
