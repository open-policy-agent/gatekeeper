package expand

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/open-policy-agent/gatekeeper/v3/cmd/gator/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/expand"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	examples = `# expand resources in a manifest
gator expand --filename="manifest.yaml"

# expand a directory
gator expand --filename="config-and-policies/"

# Use multiple inputs
gator expand --filename="manifest.yaml" --filename="templates-and-constraints/"

# Output JSON to file
gator expand --filename="manifest.yaml" --format=json --outputfile=results.yaml`
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

func run(_ *cobra.Command, _ []string) {
	unstrucs, err := reader.ReadSources(flagFilenames, flagImages, flagTempDir)
	if err != nil {
		util.ErrFatalf("reading: %v", err)
	}
	if len(unstrucs) == 0 {
		util.ErrFatalf("no input data identified")
	}

	resultants, err := expand.Expand(unstrucs)
	if err != nil {
		util.ErrFatalf("error expanding resources: %v", err)
	}
	// Sort resultants for deterministic output
	sortUnstructs(resultants)

	output := resourcesToString(resultants, flagFormat)
	if flagOutput == "" {
		fmt.Println(output)
	} else {
		fmt.Printf("Writing output to file: %s\n", flagOutput)
		util.WriteToFile(output, flagOutput)
	}

	os.Exit(0)
}

func resourcetoYAMLString(resource *unstructured.Unstructured) string {
	jsonb, err := json.Marshal(resource)
	if err != nil {
		util.ErrFatalf("pre-marshaling results to json: %v", err)
	}

	unmarshalled := map[string]interface{}{}
	err = json.Unmarshal(jsonb, &unmarshalled)
	if err != nil {
		util.ErrFatalf("pre-unmarshaling results from json: %v", err)
	}

	var b bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&b)
	if err := yamlEncoder.Encode(unmarshalled); err != nil {
		util.ErrFatalf("marshaling validation yaml results: %v", err)
	}
	return b.String()
}

func resourceToJSONString(resource *unstructured.Unstructured) string {
	b, err := json.MarshalIndent(resource, "", "    ")
	if err != nil {
		util.ErrFatalf("marshaling validation json results: %v", err)
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
		util.ErrFatalf("unrecognized value for %s flag: %s", flagNameFormat, format)
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

func sortUnstructs(objs []*unstructured.Unstructured) {
	sortKey := func(o *unstructured.Unstructured) string {
		return o.GetName() + o.GetAPIVersion() + o.GetKind()
	}
	sort.Slice(objs, func(i, j int) bool {
		return sortKey(objs[i]) > sortKey(objs[j])
	})
}
