package expansion

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"sync"

	expansionunversioned "github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	mutationtypes "github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ExpansionEnabled *bool
	log              = logf.Log.WithName("expansion").WithValues(logging.Process, "expansion")
)

// maxRecursionDepth specifies the maximum call depth for recursive expansion.
// Theoretically, it should be impossible for a cycle to be created but this
// measure is put in place as a safeguard.
const maxRecursionDepth = 30

func init() {
	ExpansionEnabled = flag.Bool("enable-generator-resource-expansion", false, "(alpha) Enable the expansion of generator resources")
}

type System struct {
	lock           sync.RWMutex
	templates      map[string]*expansionunversioned.ExpansionTemplate
	mutationSystem *mutation.System
	graph          cycleDetector
}

type Resultant struct {
	Obj               *unstructured.Unstructured
	TemplateName      string
	EnforcementAction string
}

func keyForTemplate(template *expansionunversioned.ExpansionTemplate) string {
	return template.ObjectMeta.Name
}

func (s *System) UpsertTemplate(template *expansionunversioned.ExpansionTemplate) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	log.V(1).Info("upserting ExpansionTemplate", "template name", template.GetName())

	if err := ValidateTemplate(template); err != nil {
		return err
	}

	k := keyForTemplate(template)
	if oldTemp, exists := s.templates[k]; exists {
		s.graph.removeTemplate(oldTemp)
	}

	if err := s.graph.addTemplate(template); err != nil {
		return err
	}

	s.templates[k] = template.DeepCopy()
	return nil
}

func (s *System) RemoveTemplate(template *expansionunversioned.ExpansionTemplate) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	k := keyForTemplate(template)
	if k == "" {
		return fmt.Errorf("cannot remove template with empty name")
	}

	if oldTemp, exists := s.templates[k]; exists {
		log.V(1).Info("deleting old template from graph and cache", "template name", template.GetName())
		s.graph.removeTemplate(oldTemp)
		delete(s.templates, k)
	}
	return nil
}

func ValidateTemplate(template *expansionunversioned.ExpansionTemplate) error {
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
func (s *System) Expand(base *mutationtypes.Mutable) ([]*Resultant, error) {
	var res []*Resultant
	if err := s.expandRecursive(base, &res, 0); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *System) expandRecursive(base *mutationtypes.Mutable, resultants *[]*Resultant, depth int) error {
	if depth >= maxRecursionDepth {
		return fmt.Errorf("maximum recursion depth of %d reached", maxRecursionDepth)
	}

	res, err := s.expand(base)
	if err != nil {
		return err
	}

	for _, r := range res {
		mut := &mutationtypes.Mutable{
			Object:    r.Obj,
			Namespace: base.Namespace,
			Username:  base.Username,
			Source:    base.Source,
		}
		if err := s.expandRecursive(mut, resultants, depth+1); err != nil {
			return err
		}
	}

	*resultants = append(*resultants, res...)
	return nil
}

func (s *System) expand(base *mutationtypes.Mutable) ([]*Resultant, error) {
	gvk := base.Object.GroupVersionKind()
	if gvk == (schema.GroupVersionKind{}) {
		return nil, fmt.Errorf("cannot expand resource %s with empty GVK", base.Object.GetName())
	}

	var resultants []*Resultant
	templates := s.templatesForGVK(gvk)

	for _, te := range templates {
		res, err := expandResource(base.Object, base.Namespace, te)
		resultants = append(resultants, &Resultant{
			Obj:               res,
			TemplateName:      te.ObjectMeta.Name,
			EnforcementAction: te.Spec.EnforcementAction,
		})
		if err != nil {
			return nil, err
		}
	}

	if s.mutationSystem == nil {
		return resultants, nil
	}

	for _, res := range resultants {
		mutable := &mutationtypes.Mutable{
			Object:    res.Obj,
			Namespace: base.Namespace,
			Username:  base.Username,
			Source:    mutationtypes.SourceTypeGenerated,
		}
		_, err := s.mutationSystem.Mutate(mutable)
		if err != nil {
			return nil, fmt.Errorf("failed to mutate resultant resource %s: %w", res.Obj.GetName(), err)
		}
	}

	return resultants, nil
}

func expandResource(obj *unstructured.Unstructured, ns *corev1.Namespace, template *expansionunversioned.ExpansionTemplate) (*unstructured.Unstructured, error) {
	if ns == nil {
		return nil, fmt.Errorf("cannot expand resource with nil namespace")
	}

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
		return nil, fmt.Errorf("could not extract source field from unstructured: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("could not find source field %q in resource %s", srcPath, obj.GetName())
	}

	resource := &unstructured.Unstructured{}
	resource.SetUnstructuredContent(src)
	resource.SetGroupVersionKind(resultantGVK)
	resource.SetNamespace(ns.Name)
	resource.SetName(mockNameForResource(obj, resultantGVK))

	return resource, nil
}

// mockNameForResource returns a mock name for a resultant resource created
// from expanding `gen`. The name will be of the form:
// "<generator name>-<resultant kind>". For example, a deployment named
// `nginx-deployment` will produce a resultant named `nginx-deployment-pod`.
func mockNameForResource(gen *unstructured.Unstructured, gvk schema.GroupVersionKind) string {
	name := gen.GetName()
	if gvk.Kind != "" {
		name += "-"
	}
	name += gvk.Kind

	return strings.ToLower(name)
}

func NewSystem(mutationSystem *mutation.System) *System {
	return &System{
		lock:           sync.RWMutex{},
		templates:      map[string]*expansionunversioned.ExpansionTemplate{},
		mutationSystem: mutationSystem,
		graph:          make(gvkGraph),
	}
}
