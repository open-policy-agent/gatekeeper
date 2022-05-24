package expand

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/gatekeeper/pkg/expansion"
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

var allowedExtensions = []string{".yaml", ".yml", ".json"}

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
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "n", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.  Cannot be used in tandem with stdin.")
	Cmd.Flags().StringVarP(&flagFormat, flagNameFormat, "f", "", fmt.Sprintf("Output format.  One of: %s|%s.", stringJSON, stringYAML))
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", fmt.Sprintf("Output file path. If the file already exists, it will be overwritten."))
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := readSources(flagFilenames)
	if err != nil {
		errFatalf("reading: %v\n", err)
	}
	if len(unstrucs) == 0 {
		errFatalf("no input data identified\n")
	}

	resultants, err := expansion.ExpandResources(unstrucs)
	if err != nil {
		fmt.Println(err)
	}

	if flagOutput == "" {
		printResources(resultants)
	} else {
		fmt.Printf("Writing output to file: %s\n", flagOutput)
		err = resourcesToFile(resultants, flagOutput)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}

func printResources(resources []*unstructured.Unstructured) {
	fmt.Println()
	for i, res := range resources {
		fmt.Printf(prettyPrint(res))
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
		fmt.Fprintf(file, prettyPrint(res))
		if i != len(resources)-1 {
			fmt.Fprintln(file, delimeter)
		}
	}

	return nil
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
