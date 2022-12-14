package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/open-policy-agent/gatekeeper/pkg/gator/reader"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/test"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	examples = `  # test a manifest containing Kubernetes objects, Constraint Templates, and Constraints
  gator test --filename="manifest.yaml"

  # test a directory
  gator test --filename="config-and-policies/"

  # Use multiple inputs
  gator test --filename="manifest.yaml" --filename="templates-and-constraints/"

  # Receive input from stdin
  cat manifest.yaml | gator test

  # Output structured violations data
  gator test --filename="manifest.yaml" --output=json

  Note: The alpha "gator test" has been renamed to "gator verify".  "gator
  verify" verifies individual Constraint Templates against suites of tests, where "gator
  test" evaluates sets of resources against sets of Constraints and Templates.`
)

var Cmd = &cobra.Command{
	Use:     "test",
	Short:   "test evaluates resources against policies as defined by constraint templates and constraints. Note: The alpha `gator test` has been renamed to `gator verify`.",
	Example: examples,
	Run:     run,
	Args:    cobra.NoArgs,
}

var (
	flagFilenames    []string
	flagOutput       string
	flagIncludeTrace bool
	flagImages       []string
	flagTempDir      string
)

const (
	flagNameFilename = "filename"
	flagNameOutput   = "output"
	flagNameImage    = "image"
	flagNameTempDir  = "tempdir"

	stringJSON          = "json"
	stringYAML          = "yaml"
	stringHumanFriendly = "default"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", fmt.Sprintf("Output format.  One of: %s|%s.", stringJSON, stringYAML))
	Cmd.Flags().BoolVarP(&flagIncludeTrace, "trace", "t", false, `include a trace for the underlying constraint framework evaluation`)
	Cmd.Flags().StringArrayVarP(&flagImages, flagNameImage, "i", []string{}, "a URL to an OCI image containing policies. Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagTempDir, flagNameTempDir, "d", "", fmt.Sprintf("Specifies the temporary directory to download and unpack images to, if using the --%s flag. Optional.", flagNameImage))
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := reader.ReadSources(flagFilenames, flagImages, flagTempDir)
	if err != nil {
		errFatalf("reading: %v", err)
	}
	if len(unstrucs) == 0 {
		errFatalf("no input data identified")
	}

	responses, err := test.Test(unstrucs, flagIncludeTrace)
	if err != nil {
		errFatalf("auditing objects: %v\n", err)
	}
	results := responses.Results()

	fmt.Print(formatOutput(flagOutput, results))

	// Whether or not we return non-zero depends on whether we have a `deny`
	// enforcementAction on one of the violated constraints
	exitCode := 0
	if enforceableFailure(results) {
		exitCode = 1
	}
	os.Exit(exitCode)
}

func formatOutput(flagOutput string, results []*test.GatorResult) string {
	switch strings.ToLower(flagOutput) {
	case stringJSON:
		b, err := json.MarshalIndent(results, "", "    ")
		if err != nil {
			errFatalf("marshaling validation json results: %v", err)
		}
		return string(b)
	case stringYAML:
		yamlResults := test.GetYamlFriendlyResults(results)
		jsonb, err := json.Marshal(yamlResults)
		if err != nil {
			errFatalf("pre-marshaling results to json: %v", err)
		}

		unmarshalled := []*test.YamlGatorResult{}
		err = json.Unmarshal(jsonb, &unmarshalled)
		if err != nil {
			errFatalf("pre-unmarshaling results from json: %v", err)
		}

		yamlb, err := yaml.Marshal(unmarshalled)
		if err != nil {
			errFatalf("marshaling validation yaml results: %v", err)
		}
		return string(yamlb)
	case stringHumanFriendly:
	default:
		var buf bytes.Buffer
		if len(results) > 0 {
			for _, result := range results {
				buf.WriteString(fmt.Sprintf("[%q] Message: %q \n", result.Constraint.GetName(), result.Msg))

				if result.Trace != nil {
					buf.WriteString(fmt.Sprintf("Trace: %v", *result.Trace))
				}
			}
		}
		return buf.String()
	}

	return ""
}

func enforceableFailure(results []*test.GatorResult) bool {
	for _, result := range results {
		if result.EnforcementAction == string(util.Deny) {
			return true
		}
	}

	return false
}

func errFatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}
