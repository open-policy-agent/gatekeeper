/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package validate

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/open-policy-agent/gatekeeper/pkg/gator/validate"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Cmd is the gator validate subcommand.
// TODO(juliankatz): write the description and add an examples block
var Cmd = &cobra.Command{
	Use:   "validate",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: run,
}

var filenames []string

const flagNameFilename = "filename"

func init() {
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// validateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// validateCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	Cmd.Flags().StringArrayVarP(&filenames, flagNameFilename, "f", []string{}, "a file containing yaml kubernetes resources.  Can be specified multiple times.  Cannot be used in tandem with stdin.")
}

func run(cmd *cobra.Command, args []string) {
	var unstrucs []*unstructured.Unstructured

	// check if stdin has data
	stdinfo, err := os.Stdin.Stat()
	if err != nil {
		exitf("getting info for stdout: %w", err)
	}

	// using stdin in combination with flags is not supported
	if stdinfo.Size() > 0 && len(filenames) > 0 {
		exitf("stdin cannot be used in combination with %q flag", flagNameFilename)
	}

	// if no files specified, read from Stdin
	if stdinfo.Size() > 0 {
		us, err := readYAMLSource(os.Stdin)
		if err != nil {
			exitf("reading from stdin: %w", err)
		}
		unstrucs = append(unstrucs, us...)
	} else if len(filenames) > 0 {
		for _, filename := range filenames {
			file, err := os.Open(filename)
			if err != nil {
				exitf("opening file %q: %w", filename, err)
			}

			us, err := readYAMLSource(bufio.NewReader(file))
			if err != nil {
				exitf("reading from file %q: %w", filename, err)
			}
			file.Close()

			unstrucs = append(unstrucs, us...)
		}
	} else {
		exitf("must include data via either stdin or the %q flag", flagNameFilename)
	}

	responses, err := validate.Validate(unstrucs)
	if err != nil {
		exitf("auditing objects: %v\n", err)
	}

	results := responses.Results()
	if len(results) > 0 {
		for _, result := range results {
			fmt.Printf("Message: %q", result.Msg)
		}

		os.Exit(1)
	}
}

func exitf(format string, a ...interface{}) {
	fmt.Println(fmt.Errorf(format, a...))
	os.Exit(1)
}

// func stdinHasData() (bool, error) {
// 	info, err := os.Stdin.Stat()
// 	if err != nil {
// 		return false, fmt.Errorf("getting info for stdout: %w", err)
// 	}
//
// 	return info.Size() > 0, nil
// }

func readYAMLSource(r io.Reader) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured

	decoder := yaml.NewYAMLOrJSONDecoder(r, 1000)
	for {
		u := &unstructured.Unstructured{
			Object: make(map[string]interface{}),
		}
		err := decoder.Decode(&u.Object)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading chunk: %w\n", err)
		}

		objs = append(objs, u)
	}

	return objs, nil
}
