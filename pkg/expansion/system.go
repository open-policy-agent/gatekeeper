package expansion

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	expansionunversioned "github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	mutationtypes "github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type System struct {
	lock           sync.RWMutex
	templates      map[string]*expansionunversioned.ExpansionTemplate
	mutationSystem *mutation.System
}

func keyForTemplate(template *expansionunversioned.ExpansionTemplate) string {
	return template.ObjectMeta.Name
}

func (s *System) UpsertTemplate(template *expansionunversioned.ExpansionTemplate) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := validateTemplate(template); err != nil {
		return err
	}

	s.templates[keyForTemplate(template)] = template.DeepCopy()
	return nil
}

func (s *System) RemoveTemplate(template *expansionunversioned.ExpansionTemplate) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	k := keyForTemplate(template)
	if k == "" {
		return fmt.Errorf("cannot remove template with empty name")
	}

	delete(s.templates, k)
	return nil
}

func validateTemplate(template *expansionunversioned.ExpansionTemplate) error {
	k := keyForTemplate(template)
	if k == "" {
		return fmt.Errorf("ExpansionTemplate has empty name field")
	}
	if template.Spec.TemplateSource == "" {
		return fmt.Errorf("ExpansionTemplate %s has empty source field", k)
	}
	if template.Spec.GeneratedGVK == (expansionunversioned.GeneratedGVK{}) {
		return fmt.Errorf("ExpansionTemplate %s has empty generatedGVK field", k)
	}
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

// templatesForGVK returns a slice of ExpansionTemplates that match a given gvk.
func (s *System) templatesForGVK(gvk schema.GroupVersionKind) []*expansionunversioned.ExpansionTemplate {
	s.lock.RLock()
	defer s.lock.RUnlock()

	var templates []*expansionunversioned.ExpansionTemplate
	for _, t := range s.templates {
		for _, apply := range t.Spec.ApplyTo {
			if apply.Matches(gvk) {
				templates = append(templates, t)
			}
		}
	}

	return templates
}

// Expand expands `base` into resultant resources, and applies any applicable
// mutators. If no ExpansionTemplates match `base`, an empty slice
// will be returned. If `s.mutationSystem` is nil, no mutations will be applied.
func (s *System) Expand(base *mutationtypes.Mutable) ([]*unstructured.Unstructured, error) {
	gvk := base.Object.GroupVersionKind()
	if gvk == (schema.GroupVersionKind{}) {
		return nil, fmt.Errorf("cannot expand resource %s with empty GVK", base.Object.GetName())
	}
	var resultants []*unstructured.Unstructured
	templates := s.templatesForGVK(gvk)

	for _, te := range templates {
		res, err := expandResource(base.Object, base.Namespace, te)
		resultants = append(resultants, res)
		if err != nil {
			return nil, err
		}
	}

	if s.mutationSystem == nil {
		return resultants, nil
	}

	for _, res := range resultants {
		mutable := &mutationtypes.Mutable{
			Object:    res,
			Namespace: base.Namespace,
			Username:  base.Username,
			Source:    mutationtypes.SourceTypeGenerated,
		}
		_, err := s.mutationSystem.Mutate(mutable)
		if err != nil {
			return nil, fmt.Errorf("failed to mutate resultant resource %s: %s", res.GetName(), err)
		}
	}

	return resultants, nil
}

func expandResource(obj *unstructured.Unstructured, ns *corev1.Namespace, template *expansionunversioned.ExpansionTemplate) (*unstructured.Unstructured, error) {
	srcPath := template.Spec.TemplateSource
	if srcPath == "" {
		return nil, fmt.Errorf("cannot expand resource using a template with no source")
	}
	resultantGVK := genGVKToSchemaGVK(template.Spec.GeneratedGVK)
	if resultantGVK == (schema.GroupVersionKind{}) {
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
	resource.SetNamespace(ns.Name)

	return resource, nil
}

func NewSystem(mutationSystem *mutation.System) *System {
	return &System{
		lock:           sync.RWMutex{},
		templates:      map[string]*expansionunversioned.ExpansionTemplate{},
		mutationSystem: mutationSystem,
	}
}
