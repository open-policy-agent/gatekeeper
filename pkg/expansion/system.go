package expansion

import (
	"fmt"
	"sync"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type System struct {
	lock      sync.Mutex
	mutators  map[types.ID]types.Mutator
	templates map[string]*mutationsunversioned.TemplateExpansion
}

func keyForTemplate(template *mutationsunversioned.TemplateExpansion) string {
	return template.ObjectMeta.Name
}

func (s *System) UpsertTemplate(template *mutationsunversioned.TemplateExpansion) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	k := keyForTemplate(template)
	if k == "" {
		return fmt.Errorf("cannot upsert template with empty name")
	}

	s.templates[k] = template.DeepCopy()
	return nil
}

func (s *System) RemoveTemplate(template *mutationsunversioned.TemplateExpansion) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	k := keyForTemplate(template)
	if k == "" {
		return fmt.Errorf("cannot remove template with empty name")
	}

	delete(s.templates, k)
	return nil
}

func (s *System) UpsertMutator(mut types.Mutator) error {
	if !mut.ExpandsGenerators() {
		return fmt.Errorf("cannot add mutator to cache that does not have 'origin: Generated' field")
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	k := mut.ID()
	emptyID := types.ID{}
	if k == emptyID {
		return fmt.Errorf("cannot upsert mutator with empty ID")
	}

	s.mutators[k] = mut
	return nil
}

func (s *System) RemoveMutator(mut types.Mutator) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	k := mut.ID()
	emptyID := types.ID{}
	if k == emptyID {
		return fmt.Errorf("cannot remove mutator with empty ID")
	}

	delete(s.mutators, k)
	return nil
}

// MutatorsForGVK returns a slice of mutators that apply to specified GVK.
func (s *System) MutatorsForGVK(gvk schema.GroupVersionKind) []types.Mutator {
	s.lock.Lock()
	defer s.lock.Unlock()

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	mutable := &types.Mutable{Object: u}
	var muts []types.Mutator
	for _, mut := range s.mutators {
		if mut.Matches(mutable) {
			muts = append(muts, mut)
		}
	}

	return muts
}

// TemplatesForGVK returns a slice of TemplateExpansions that match a given gvk.
func (s *System) TemplatesForGVK(gvk schema.GroupVersionKind) []mutationsunversioned.TemplateExpansion {
	s.lock.Lock()
	defer s.lock.Unlock()

	var templates []mutationsunversioned.TemplateExpansion
	for _, t := range s.templates {
		for _, apply := range t.Spec.ApplyTo {
			if apply.Matches(gvk) {
				templates = append(templates, *t)
			}
		}
	}

	return templates
}

func NewExpansionCache(mutators []types.Mutator, templates []*mutationsunversioned.TemplateExpansion) (*System, error) {
	ec := &System{
		lock:      sync.Mutex{},
		mutators:  map[types.ID]types.Mutator{},
		templates: map[string]*mutationsunversioned.TemplateExpansion{},
	}

	for _, m := range mutators {
		if !m.ExpandsGenerators() {
			continue
		}
		if err := ec.UpsertMutator(m); err != nil {
			return nil, err
		}
	}
	for _, t := range templates {
		if err := ec.UpsertTemplate(t); err != nil {
			return nil, err
		}
	}

	return ec, nil
}

func V1Alpha1TemplateToUnversioned(expansion *v1alpha1.TemplateExpansion) *mutationsunversioned.TemplateExpansion {
	return &mutationsunversioned.TemplateExpansion{
		TypeMeta:   expansion.TypeMeta,
		ObjectMeta: expansion.ObjectMeta,
		Spec: mutationsunversioned.TemplateExpansionSpec{
			ApplyTo:        expansion.Spec.ApplyTo,
			TemplateSource: expansion.Spec.TemplateSource,
			GeneratedGVK: mutationsunversioned.GeneratedGVK{
				Group:   expansion.Spec.GeneratedGVK.Group,
				Version: expansion.Spec.GeneratedGVK.Version,
				Kind:    expansion.Spec.GeneratedGVK.Kind,
			},
		},
		Status: mutationsunversioned.TemplateExpansionStatus{},
	}
}
