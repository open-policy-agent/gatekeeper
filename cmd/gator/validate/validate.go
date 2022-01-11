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

const (
	examples = `  # Validate a manifest containing Kubernetes objects, Constraint Templates, and Constraints
  gator validate --filename="manifest.yaml"

  # Validate a directory
  gator validate --filename="config-and-policies/"

  # Use multiple inputs
  gator validate --filename="manifest.yaml" --filename="templates-and-constraints/"

  # Receive input from stdin
  cat manifest.yaml | gator validate

  # Output structured violations data
  gator validate --filename="manifest.yaml" --json`
)

var Cmd = &cobra.Command{
	Use:     "validate",
	Short:   "validate resources against templates and constraints",
	Example: examples,
	Run:     run,
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
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file or directory containing kubernetes resources.  Can be specified multiple times.  Cannot be used in tandem with stdin.")
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
		// normalize directories by listing their files
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
