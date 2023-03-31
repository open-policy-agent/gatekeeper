package expand

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/open-policy-agent/gatekeeper/pkg/gator/expand"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/reader"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2" // yaml.v3 inserts a space before '-', which is inconsistent with standard, kubernetes and kubebuilder format. yaml.v2 does not insert these spaces.
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
	flagImages    []string
	flagTempDir   string
)

const (
	flagNameFilename = "filename"
	flagNameFormat   = "format"
	flagNameOutput   = "outputfile"
	flagNameImage    = "image"
	flagNameTempDir  = "tempdir"

	stringJSON = "json"
	stringYAML = "yaml"

	delimeter = "---"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "n", []string{}, "a file or directory containing Kubernetes resources.  Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagFormat, flagNameFormat, "f", "", fmt.Sprintf("Output format.  One of: %s|%s.", stringJSON, stringYAML))
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", "Output file path. If the file already exists, it will be overwritten.")
	Cmd.Flags().StringArrayVarP(&flagImages, flagNameImage, "i", []string{}, "a URL to an OCI image containing policies. Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagTempDir, flagNameTempDir, "d", "", fmt.Sprintf("Specifies the temporary directory to download and unpack images to, if using the --%s flag. Optional.", flagNameImage))
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := reader.ReadSources(flagFilenames, flagImages, flagTempDir)
	if err != nil {
		errFatalf("reading: %v\n", err)
	}
	if len(unstrucs) == 0 {
		errFatalf("no input data identified\n")
	}

	resultants, err := expand.Expand(unstrucs)
	if err != nil {
		errFatalf("error expanding resources: %v", err)
	}
	// Sort resultants for deterministic output
	sortUnstructs(resultants)

	output := resourcesToString(resultants, flagFormat)
	if flagOutput == "" {
		fmt.Println(output)
	} else {
		fmt.Printf("Writing output to file: %s\n", flagOutput)
		stringToFile(output, flagOutput)
	}

	os.Exit(0)
}

func resourcetoYAMLString(resource *unstructured.Unstructured) string {
	jsonb, err := json.Marshal(resource)
	if err != nil {
		errFatalf("pre-marshaling results to json: %v", err)
	}

	unmarshalled := map[string]interface{}{}
	err = json.Unmarshal(jsonb, &unmarshalled)
	if err != nil {
		errFatalf("pre-unmarshaling results from json: %v", err)
	}

	var b bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&b)
	if err := yamlEncoder.Encode(unmarshalled); err != nil {
		errFatalf("marshaling validation yaml results: %v", err)
	}
	return b.String()
}

func resourceToJSONString(resource *unstructured.Unstructured) string {
	b, err := json.MarshalIndent(resource, "", "    ")
	if err != nil {
		errFatalf("marshaling validation json results: %v", err)
	}
	return string(b)
}

func resourcesToString(resources []*unstructured.Unstructured, format string) string {
	var conversionFunc func(unstructured2 *unstructured.Unstructured) string
	switch format {
	case "", stringYAML:
		conversionFunc = resourcetoYAMLString
	case stringJSON:
		conversionFunc = resourceToJSONString
	default:
		errFatalf("unrecognized value for %s flag: %s", flagNameFormat, format)
	}

	output := ""
	for i, r := range resources {
		output += conversionFunc(r)
		if i != len(resources)-1 {
			output += fmt.Sprintf("%s\n", delimeter)
		}
	}
	return output
}

func stringToFile(s string, path string) {
	file, err := os.Create(path)
	if err != nil {
		errFatalf("error creating file at path %s: %v", path, err)
	}

	if _, err = fmt.Fprint(file, s); err != nil {
		errFatalf("error writing to file at path %s: %s", path, err)
	}
}

func sortUnstructs(objs []*unstructured.Unstructured) {
	sortKey := func(o *unstructured.Unstructured) string {
		return o.GetName() + o.GetAPIVersion() + o.GetKind()
	}
	sort.Slice(objs, func(i, j int) bool {
		return sortKey(objs[i]) > sortKey(objs[j])
	})
}

func errFatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
