package expansion

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/modifyset"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var MutatorTypes = map[string]bool{"Assign": true, "AssignMetadata": true, "ModifySet": true}

func sourcePath(source string) []string {
	return strings.Split(source, ".")
}

func prettyResource(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		return string(b) + "\n"
	}
	return ""
}

func ExpandResources(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	generators, unstructMutators, unstructTemplates := sortResources(resources)

	mutators, err := convertMutators(unstructMutators)
	if err != nil {
		return nil, fmt.Errorf("error converting mutators: %s", err)
	}
	templates, err := convertTemplateExpansions(unstructTemplates)
	if err != nil {
		return nil, fmt.Errorf("error converting template expansions: %s", err)
	}

	cache, err := NewExpansionCache(mutators, templates)
	if err != nil {
		return nil, fmt.Errorf("error creating System: %s", err)
	}

	var resultants []*unstructured.Unstructured
	for _, gen := range generators {
		temps := cache.TemplatesForGVK(gen.GroupVersionKind())
		for i := 0; i < len(temps); i++ {
			t := temps[i]
			muts := cache.MutatorsForGVK(genGVKToSchemaGVK(t.Spec.GeneratedGVK))
			result, err := ExpandGenerator(gen, &t, muts)
			if err != nil {
				return nil, fmt.Errorf("error expanding generator: %s", err)
			}
			resultants = append(resultants, result)
		}
	}

	return resultants, nil
}

func ExpandGenerator(generator *unstructured.Unstructured, template *mutationsunversioned.TemplateExpansion, mutators []types.Mutator) (*unstructured.Unstructured, error) {
	srcPath := template.Spec.TemplateSource
	if srcPath == "" {
		return nil, fmt.Errorf("cannot expand generator for template with no source")
	}
	resultantGVK := genGVKToSchemaGVK(template.Spec.GeneratedGVK)
	emptyGVK := schema.GroupVersionKind{}
	if resultantGVK == emptyGVK {
		return nil, fmt.Errorf("cannot expand generator for template with empty generatedGVK")
	}

	src, ok, err := unstructured.NestedMap(generator.Object, sourcePath(srcPath)...)
	if err != nil {
		return nil, fmt.Errorf("could not extract source field from unstructured: %s", err)
	}
	if !ok {
		return nil, fmt.Errorf("could not find source field %q in generator", srcPath)
	}

	return createResultantKind(resultantGVK, src, mutators)
}

func convertUnstructuredToTyped(u *unstructured.Unstructured, obj interface{}) error {
	if u == nil {
		return fmt.Errorf("cannot convert nil unstructured to type")
	}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), obj)
	return err
}

func convertTemplateExpansion(u *unstructured.Unstructured) (mutationsunversioned.TemplateExpansion, error) {
	te := mutationsunversioned.TemplateExpansion{}
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

func sortMutators(mutators []types.Mutator) {
	sort.SliceStable(mutators, func(x, y int) bool {
		return mutatorSortKey(mutators[x]) < mutatorSortKey(mutators[y])
	})
}

func mutatorSortKey(mutator types.Mutator) string {
	return mutator.String()
}

func genGVKToSchemaGVK(gvk mutationsunversioned.GeneratedGVK) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
}

func createResultantKind(gvk schema.GroupVersionKind, source map[string]interface{}, mutators []types.Mutator) (*unstructured.Unstructured, error) {
	resource := unstructured.Unstructured{}
	resource.SetUnstructuredContent(source)
	resource.SetGroupVersionKind(gvk)
	sortMutators(mutators)

	for _, m := range mutators {
		_, err := m.Mutate(&types.Mutable{Object: &resource, Username: "kubernetes-admin"})
		if err != nil {
			return nil, err
		}
	}

	return &resource, nil
}

func convertTemplateExpansions(templates []*unstructured.Unstructured) ([]*mutationsunversioned.TemplateExpansion, error) {
	convertedTemplates := make([]*mutationsunversioned.TemplateExpansion, len(templates))
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
