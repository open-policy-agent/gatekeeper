package expand

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/modifyset"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
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

	MutatorKinds = map[string]bool{
		"Assign":         true,
		"AssignMetadata": true,
		"ModifySet":      true,
	}
	MutatorAPIVersions = map[string]bool{
		"mutations.gatekeeper.sh/v1alpha1": true,
		"mutations.gatekeeper.sh/v1beta1":  true,
	}
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
	unstrucs, err := gator.ReadSources(flagFilenames)
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
		resultants, err = expSystem.Expand(gen, "gatekeeper-admin", mutSystem)
		if err != nil {
			return nil, fmt.Errorf("error expanding generator: %s", err)
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

// sortResources sorts a list of resources into mutators, generators, template
// expansions and returns them respectively.
func sortResources(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, []*unstructured.Unstructured, []*unstructured.Unstructured) {
	var generators []*unstructured.Unstructured
	var mutators []*unstructured.Unstructured
	var templates []*unstructured.Unstructured

	for _, r := range resources {
		switch {
		case isMutator(r):
			mutators = append(mutators, r)
		case r.GetKind() == "TemplateExpansion":
			templates = append(templates, r)
		default:
			generators = append(generators, r)
		}
	}

	return generators, mutators, templates
}

func isMutator(obj *unstructured.Unstructured) bool {
	if _, exists := MutatorKinds[obj.GetKind()]; !exists {
		return false
	}
	if _, exists := MutatorAPIVersions[obj.GetAPIVersion()]; !exists {
		return false
	}

	return true
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
