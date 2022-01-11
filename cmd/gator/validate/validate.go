/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package validate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/gatekeeper/pkg/gator/validate"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

var (
	errLog *log.Logger
	outLog *log.Logger
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

var (
	flagFilenames []string
	flagYAML      bool
	flagJSON      bool
)

const (
	flagNameFilename = "filename"
	flagNameJSON     = "json"
	flagNameYAML     = "yaml"
)

func init() {
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// validateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// validateCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file containing yaml kubernetes resources.  Can be specified multiple times.  Cannot be used in tandem with stdin.")
	Cmd.Flags().BoolVar(&flagYAML, flagNameYAML, false, "print validation information as yaml.")
	Cmd.Flags().BoolVar(&flagJSON, flagNameJSON, false, "print validation information as json.")

	errLog = log.New(os.Stderr, "", 0)
	outLog = log.New(os.Stdout, "", 0)
}

func run(cmd *cobra.Command, args []string) {
	var unstrucs []*unstructured.Unstructured

	// check if stdin has data
	stdinfo, err := os.Stdin.Stat()
	if err != nil {
		errLog.Fatalf("getting info for stdout: %s", err)
	}

	// using stdin in combination with flags is not supported
	if stdinfo.Size() > 0 && len(flagFilenames) > 0 {
		errLog.Fatalf("stdin cannot be used in combination with %q flag", flagNameFilename)
	}

	// if no files specified, read from Stdin
	if stdinfo.Size() > 0 {
		us, err := readYAMLSource(os.Stdin)
		if err != nil {
			errLog.Fatalf("reading from stdin: %s", err)
		}
		unstrucs = append(unstrucs, us...)
	} else if len(flagFilenames) > 0 {
		normalized, err := normalize(flagFilenames)
		if err != nil {
			errLog.Fatalf("normalizing: %s", err)
		}

		for _, filename := range normalized {
			file, err := os.Open(filename)
			if err != nil {
				errLog.Fatalf("opening file %q: %s", filename, err)
			}

			us, err := readYAMLSource(bufio.NewReader(file))
			if err != nil {
				errLog.Fatalf("reading from file %q: %s", filename, err)
			}
			file.Close()

			unstrucs = append(unstrucs, us...)
		}
	} else {
		errLog.Fatalf("no input data: must include data via either stdin or the %q flag", flagNameFilename)
	}

	responses, err := validate.Validate(unstrucs)
	if err != nil {
		errLog.Fatalf("auditing objects: %v\n", err)
	}

	results := responses.Results()

	if flagYAML && flagJSON {
		errLog.Fatalf("cannot use --%s and --%s in combination", flagNameJSON, flagNameYAML)
	}

	if flagJSON {
		b, err := json.MarshalIndent(results, "", "    ")
		if err != nil {
			errLog.Fatalf("marshaling validation json results: %s", err)
		}
		outLog.Fatal(string(b))
	}

	if flagYAML {
		b, err := yaml.Marshal(results)
		if err != nil {
			errLog.Fatalf("marshaling validation yaml results: %s", err)
		}
		outLog.Fatal(string(b))
	}

	if len(results) > 0 {
		for _, result := range results {
			outLog.Printf("Message: %q", result.Msg)
		}

		os.Exit(1)
	}
}

func normalize(filenames []string) ([]string, error) {
	var output []string

	for _, filename := range filenames {
		err := filepath.Walk(filename, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// only add files to the normalized output
			if info.IsDir() {
				return nil
			}

			output = append(output, path)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %q: %w", filename, err)
		}
	}

	return output, nil
}

func readYAMLSource(r io.Reader) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured

	decoder := k8syaml.NewYAMLOrJSONDecoder(r, 1000)
	for {
		u := &unstructured.Unstructured{
			Object: make(map[string]interface{}),
		}
		err := decoder.Decode(&u.Object)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading yaml source: %w\n", err)
		}

		objs = append(objs, u)
	}

	return objs, nil
}
