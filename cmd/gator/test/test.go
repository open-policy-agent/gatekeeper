package test

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/pkg/gator"
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
	flagFilenames []string
	flagOutput    string
	includeTrace  bool
)

const (
	flagNameFilename = "filename"
	flagNameOutput   = "output"

	stringJSON = "json"
	stringYAML = "yaml"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", fmt.Sprintf("Output format.  One of: %s|%s.", stringJSON, stringYAML))
	Cmd.Flags().BoolVarP(&includeTrace, "trace", "t", false, `include a trace for the underlying constraint framework evaluation`)
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := gator.ReadSources(flagFilenames)
	if err != nil {
		errFatalf("reading: %v", err)
	}
	if len(unstrucs) == 0 {
		errFatalf("no input data identified")
	}

	responses, err := test.Test(unstrucs, includeTrace)
	if err != nil {
		errFatalf("auditing objects: %v\n", err)
	}
	results := responses.Results()

	switch flagOutput {
	case stringJSON:
		b, err := json.MarshalIndent(results, "", "    ")
		if err != nil {
			errFatalf("marshaling validation json results: %v", err)
		}
		fmt.Print(string(b))
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
		fmt.Print(string(yamlb))
	default:
		if len(results) > 0 {
			for _, result := range results {
				fmt.Printf("[%q] Message: %q \n", result.Constraint.GetName(), result.Msg)

				if includeTrace {
					fmt.Printf("Trace: %v", *result.Trace)
				}
			}
		}
	}

	// Whether or not we return non-zero depends on whether we have a `deny`
	// enforcementAction on one of the violated constraints
	exitCode := 0
	if enforceableFailure(results) {
		exitCode = 1
	}
	os.Exit(exitCode)
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
