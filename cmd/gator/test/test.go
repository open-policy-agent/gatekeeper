package test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/test"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

var allowedExtensions = []string{".yaml", ".yml", ".json"}

var (
	flagFilenames []string
	flagOutput    string
)

const (
	flagNameFilename = "filename"
	flagNameOutput   = "output"

	stringJSON = "json"
	stringYAML = "yaml"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.  Cannot be used in tandem with stdin.")
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", fmt.Sprintf("Output format.  One of: %s|%s.", stringJSON, stringYAML))
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := readSources(flagFilenames)
	if err != nil {
		errFatalf("reading: %v", err)
	}
	if len(unstrucs) == 0 {
		errFatalf("no input data identified")
	}

	responses, err := test.Test(unstrucs)
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
		jsonb, err := json.Marshal(results)
		if err != nil {
			errFatalf("pre-marshaling results to json: %v", err)
		}

		unmarshalled := []*types.Result{}
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
				fmt.Printf("Message: %q", result.Msg)
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

func enforceableFailure(results []*types.Result) bool {
	for _, result := range results {
		if result.EnforcementAction == string(util.Deny) {
			return true
		}
	}

	return false
}

func readSources(filenames []string) ([]*unstructured.Unstructured, error) {
	var unstrucs []*unstructured.Unstructured

	// read from flags if available
	us, err := ReadFiles(filenames)
	if err != nil {
		return nil, fmt.Errorf("reading from filenames: %w", err)
	}
	unstrucs = append(unstrucs, us...)

	// check if stdin has data.  Read if so.
	us, err = readStdin()
	if err != nil {
		return nil, fmt.Errorf("reading from stdin: %w", err)
	}
	unstrucs = append(unstrucs, us...)

	return unstrucs, nil
}

func ReadFiles(filenames []string) ([]*unstructured.Unstructured, error) {
	var unstrucs []*unstructured.Unstructured

	// verify that the filenames aren't themselves disallowed extensions.  This
	// yields a much better user experience when the user mis-uses the
	// --filename flag.
	for _, name := range filenames {
		// make sure it's a file, not a directory
		fileInfo, err := os.Stat(name)
		if err != nil {
			return nil, fmt.Errorf("stat on path %q: %w", name, err)
		}

		if fileInfo.IsDir() {
			continue
		}
		if !allowedExtension(name) {
			return nil, fmt.Errorf("path %q must be of extensions: %v", name, allowedExtensions)
		}
	}

	// normalize directories by listing their files
	normalized, err := normalize(filenames)
	if err != nil {
		return nil, fmt.Errorf("normalizing filenames: %w", err)
	}

	for _, filename := range normalized {
		file, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("opening file %q: %w", filename, err)
		}
		defer file.Close()

		us, err := gator.ReadK8sResources(bufio.NewReader(file))
		if err != nil {
			return nil, fmt.Errorf("reading file %q: %w", filename, err)
		}

		unstrucs = append(unstrucs, us...)
	}

	return unstrucs, nil
}

func readStdin() ([]*unstructured.Unstructured, error) {
	stdinfo, err := os.Stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("getting stdin info: %w", err)
	}

	if stdinfo.Size() == 0 {
		return nil, nil
	}

	us, err := gator.ReadK8sResources(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("reading: %w", err)
	}

	return us, nil
}

func normalize(filenames []string) ([]string, error) {
	var output []string

	for _, filename := range filenames {
		paths, err := filesBelow(filename)
		if err != nil {
			return nil, fmt.Errorf("filename %q: %w", filename, err)
		}
		output = append(output, paths...)
	}

	return output, nil
}

// filesBelow walks the filetree from startPath and below, collecting a list of
// all the filepaths.  Directories are excluded.
func filesBelow(startPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(startPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// only add files to the normalized output
		if info.IsDir() {
			return nil
		}

		// make sure the file extension is valid
		if !allowedExtension(path) {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking: %w", err)
	}

	return files, nil
}

func allowedExtension(path string) bool {
	for _, ext := range allowedExtensions {
		if ext == filepath.Ext(path) {
			return true
		}
	}

	return false
}

func errFatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}
