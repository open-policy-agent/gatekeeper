package expand

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	examples = `  # expand resources in a manifest
  gator expand --filename="manifest.yaml"

  # expand a directory
  gator expand --filename="config-and-policies/"

  # Use multiple inputs
  gator expand --filename="manifest.yaml" --filename="templates-and-constraints/"

  # Output JSON to file
  gator expand --filename="manifest.yaml" --format=json --outputfile=results.yaml `
)

var Cmd = &cobra.Command{
	Use:     "expand",
	Short:   "expand allow for testing generator resource expansion configs by expanding a generator resource and outputting the resultant resource(s)",
	Example: examples,
	Run:     run,
	Args:    cobra.ExactArgs(0),
}

var (
	flagFilenames []string
	flagFormat    string
	flagOutput    string
)

const (
	flagNameFilename = "filename"
	flagNameFormat   = "format"
	flagNameOutput   = "outputfile"

	stringJSON = "json"
	stringYAML = "yaml"

	delimeter = "---"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "n", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagFormat, flagNameFormat, "f", "", fmt.Sprintf("Output format.  One of: %s|%s.", stringJSON, stringYAML))
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", "Output file path. If the file already exists, it will be overwritten.")
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := gator.ReadSources(flagFilenames)
	if err != nil {
		errFatalf("reading: %v\n", err)
	}
	if len(unstrucs) == 0 {
		errFatalf("no input data identified\n")
	}

	resultants, err := gator.Expand(unstrucs)
	if err == nil {
		if flagOutput == "" {
			printResources(resultants)
		} else {
			fmt.Printf("Writing output to file: %s\n", flagOutput)
			err = resourcesToFile(resultants, flagOutput)
		}
	}

	if err != nil {
		errFatalf(err.Error())
	} else {
		os.Exit(0)
	}
}

func printResources(resources []*unstructured.Unstructured) {
	fmt.Println()
	for i, res := range resources {
		fmt.Print(prettyPrint(res))
		if i != len(resources)-1 {
			fmt.Println(delimeter)
		}
	}
}

func resourcesToFile(resources []*unstructured.Unstructured, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	for i, res := range resources {
		if _, err = fmt.Fprint(file, prettyPrint(res)); err != nil {
			return err
		}
		if i != len(resources)-1 {
			if _, err = fmt.Fprintln(file, delimeter); err != nil {
				return err
			}
		}
	}

	return nil
}

func prettyPrint(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		return string(b) + "\n"
	}
	return ""
}

func errFatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}
