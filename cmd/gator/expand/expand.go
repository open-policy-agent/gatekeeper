package expand

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/modifyset"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	mutationtypes "github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

	allowedExtensions = []string{".yaml", ".yml", ".json"}
	MutatorTypes      = map[string]bool{"Assign": true, "AssignMetadata": true, "ModifySet": true}
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
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", "Output file path. If the file already exists, it will be overwritten.")
}

func run(cmd *cobra.Command, args []string) {
	unstrucs, err := readSources(flagFilenames)
	if err != nil {
		errFatalf("reading: %v\n", err)
	}
	if len(unstrucs) == 0 {
		errFatalf("no input data identified\n")
	}

	resultants, err := expandResources(unstrucs)
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

func expandResources(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	expSystem := expansion.NewSystem()
	mutSystem := mutation.NewSystem(mutation.SystemOpts{})
	generators, unstructMutators, unstructTemplates := sortResources(resources)

	mutators, err := convertMutators(unstructMutators)
	if err != nil {
		return nil, fmt.Errorf("error converting mutators: %s", err)
	}
	for _, mut := range mutators {
		if err := mutSystem.Upsert(mut); err != nil {
			return nil, fmt.Errorf("error upserting mutation: %s", err)
		}
	}

	templates, err := convertTemplateExpansions(unstructTemplates)
	if err != nil {
		return nil, fmt.Errorf("error converting template expansions: %s", err)
	}
	for _, t := range templates {
		if err := expSystem.UpsertTemplate(t); err != nil {
			return nil, fmt.Errorf("error upserting template: %s", err)
		}
	}

	var resultants []*unstructured.Unstructured
	for _, gen := range generators {
		temps := expSystem.TemplatesForGVK(gen.GroupVersionKind())
		resultants, err = expSystem.ExpandGenerator(gen, temps)
		if err != nil {
			return nil, fmt.Errorf("error expanding generator: %s", err)
		}
		for _, res := range resultants {
			mutable := &mutationtypes.Mutable{
				Object:   res,
				Username: "gatekeeper-admin",
			}
			_, err = mutSystem.Mutate(mutable, mutationtypes.SourceTypeGenerated)
			if err != nil {
				return nil, fmt.Errorf("failed to mutate resultant resource: %s", err)
			}
		}
	}

	return resultants, nil
}

func convertTemplateExpansions(templates []*unstructured.Unstructured) ([]*unversioned.TemplateExpansion, error) {
	convertedTemplates := make([]*unversioned.TemplateExpansion, len(templates))
	for i, t := range templates {
		te, err := convertTemplateExpansion(t)
		if err != nil {
			return nil, err
		}
		convertedTemplates[i] = &te
	}

	return convertedTemplates, nil
}

func convertMutators(mutators []*unstructured.Unstructured) ([]types.Mutator, error) {
	var muts []types.Mutator

	for _, m := range mutators {
		switch m.GetKind() {
		case "Assign":
			a, err := convertAssign(m)
			if err != nil {
				return nil, err
			}
			mut, err := assign.MutatorForAssign(&a)
			if err != nil {
				return nil, err
			}
			muts = append(muts, mut)
		case "AssignMetadata":
			a, err := convertAssignMetadata(m)
			if err != nil {
				return nil, err
			}
			mut, err := assignmeta.MutatorForAssignMetadata(&a)
			if err != nil {
				return nil, err
			}
			muts = append(muts, mut)
		case "ModifySet":
			ms, err := convertModifySet(m)
			if err != nil {
				return nil, err
			}
			mut, err := modifyset.MutatorForModifySet(&ms)
			if err != nil {
				return nil, err
			}
			muts = append(muts, mut)
		default:
			return muts, fmt.Errorf("cannot convert mutator of kind %q", m.GetKind())
		}
	}

	return muts, nil
}

func convertUnstructuredToTyped(u *unstructured.Unstructured, obj interface{}) error {
	if u == nil {
		return fmt.Errorf("cannot convert nil unstructured to type")
	}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), obj)
	return err
}

func convertTemplateExpansion(u *unstructured.Unstructured) (unversioned.TemplateExpansion, error) {
	te := unversioned.TemplateExpansion{}
	err := convertUnstructuredToTyped(u, &te)
	return te, err
}

func convertAssign(u *unstructured.Unstructured) (mutationsunversioned.Assign, error) {
	a := mutationsunversioned.Assign{}
	err := convertUnstructuredToTyped(u, &a)
	return a, err
}

func convertAssignMetadata(u *unstructured.Unstructured) (mutationsunversioned.AssignMetadata, error) {
	am := mutationsunversioned.AssignMetadata{}
	err := convertUnstructuredToTyped(u, &am)
	return am, err
}

func convertModifySet(u *unstructured.Unstructured) (mutationsunversioned.ModifySet, error) {
	ms := mutationsunversioned.ModifySet{}
	err := convertUnstructuredToTyped(u, &ms)
	return ms, err
}

// sortResources sorts a list of resources into mutators, generators, template expansions and return
// them respectively.
func sortResources(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, []*unstructured.Unstructured, []*unstructured.Unstructured) {
	var generators []*unstructured.Unstructured
	var mutators []*unstructured.Unstructured
	var templates []*unstructured.Unstructured

	for _, r := range resources {
		k := r.GetKind()
		_, isMutator := MutatorTypes[k]
		switch {
		case isMutator:
			mutators = append(mutators, r)
		case k == "TemplateExpansion":
			templates = append(templates, r)
		default:
			generators = append(generators, r)
		}
	}

	return generators, mutators, templates
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
		fmt.Fprint(file, prettyPrint(res))
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
