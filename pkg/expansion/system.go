package expansion

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	expansionunversioned "github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/apis/expansion/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	mutationtypes "github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type System struct {
	lock      sync.RWMutex
	templates map[string]*expansionunversioned.TemplateExpansion
}

func keyForTemplate(template *expansionunversioned.TemplateExpansion) string {
	return template.ObjectMeta.Name
}

func (s *System) UpsertTemplate(template *expansionunversioned.TemplateExpansion) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	k := keyForTemplate(template)
	if k == "" {
		return fmt.Errorf("cannot upsert template with empty name")
	}

	s.templates[k] = template.DeepCopy()
	return nil
}

func (s *System) RemoveTemplate(template *expansionunversioned.TemplateExpansion) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	k := keyForTemplate(template)
	if k == "" {
		return fmt.Errorf("cannot remove template with empty name")
	}

	delete(s.templates, k)
	return nil
}

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

func genGVKToSchemaGVK(gvk expansionunversioned.GeneratedGVK) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
}

// templatesForGVK returns a slice of TemplateExpansions that match a given gvk.
func (s *System) templatesForGVK(gvk schema.GroupVersionKind) []*expansionunversioned.TemplateExpansion {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var templates []*expansionunversioned.TemplateExpansion
	for _, t := range s.templates {
		for _, apply := range t.Spec.ApplyTo {
			if apply.Matches(gvk) {
				templates = append(templates, t)
			}
		}
	}

	return templates
}

// Expand expands `obj` with into resultant resources, and applies any applicable
// mutators. If no TemplateExpansions match `obj`, an empty slice
// will be returned. If `mutationSystem` is nil, no mutations will be applied.
// `user` is the username that should be used for mutation.
func (s *System) Expand(obj *unstructured.Unstructured, user string, mutationSystem *mutation.System) ([]*unstructured.Unstructured, error) {
	gvk := obj.GroupVersionKind()
	if gvk == (schema.GroupVersionKind{}) {
		return nil, fmt.Errorf("cannot expandResource object with empty GVK")
	}
	templates := s.templatesForGVK(gvk)
	var resultants []*unstructured.Unstructured

	for _, te := range templates {
		res, err := expandResource(obj, te)
		resultants = append(resultants, res)
		if err != nil {
			return nil, err
		}
	}

	if mutationSystem == nil {
		return resultants, nil
	}

	for _, res := range resultants {
		mutable := &mutationtypes.Mutable{
			Object:    res,
			Namespace: extractNs(res),
			Username:  user,
		}
		_, err := mutationSystem.Mutate(mutable, mutationtypes.SourceTypeGenerated)
		if err != nil {
			return nil, fmt.Errorf("failed to mutate resultant resource: %s", err)
		}
	}

	return resultants, nil
}

func expandResource(obj *unstructured.Unstructured, template *expansionunversioned.TemplateExpansion) (*unstructured.Unstructured, error) {
	srcPath := template.Spec.TemplateSource
	if srcPath == "" {
		return nil, fmt.Errorf("cannot expand resource using a template with no source")
	}
	resultantGVK := genGVKToSchemaGVK(template.Spec.GeneratedGVK)
	emptyGVK := schema.GroupVersionKind{}
	if resultantGVK == emptyGVK {
		return nil, fmt.Errorf("cannot expand resource using template with empty generatedGVK")
	}

	src, ok, err := unstructured.NestedMap(obj.Object, sourcePath(srcPath)...)
	if err != nil {
		return nil, fmt.Errorf("could not extract source field from unstructured: %s", err)
	}
	if !ok {
		return nil, fmt.Errorf("could not find source field %q in obj", srcPath)
	}

	resource := &unstructured.Unstructured{}
	resource.SetUnstructuredContent(src)
	resource.SetGroupVersionKind(resultantGVK)

	return resource, nil
}

func extractNs(obj *unstructured.Unstructured) *v1.Namespace {
	ns := &v1.Namespace{}
	ns.SetName(obj.GetNamespace())
	return ns
}

func NewSystem() *System {
	return &System{
		lock:      sync.RWMutex{},
		templates: map[string]*expansionunversioned.TemplateExpansion{},
	}
}

func V1Alpha1TemplateToUnversioned(expansion *v1alpha1.TemplateExpansion) *expansionunversioned.TemplateExpansion {
	return &expansionunversioned.TemplateExpansion{
		TypeMeta:   expansion.TypeMeta,
		ObjectMeta: expansion.ObjectMeta,
		Spec: expansionunversioned.TemplateExpansionSpec{
			ApplyTo:        expansion.Spec.ApplyTo,
			TemplateSource: expansion.Spec.TemplateSource,
			GeneratedGVK: expansionunversioned.GeneratedGVK{
				Group:   expansion.Spec.GeneratedGVK.Group,
				Version: expansion.Spec.GeneratedGVK.Version,
				Kind:    expansion.Spec.GeneratedGVK.Kind,
			},
		},
		Status: expansionunversioned.TemplateExpansionStatus{},
	}
}
