package expansion

import (
	"fmt"
	"sync"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ExpansionCache struct {
	lock      sync.Mutex
	mutators  map[types.ID]types.Mutator
	templates map[string]*mutationsunversioned.TemplateExpansion
}

func (ec *ExpansionCache) UpsertTemplate(template *mutationsunversioned.TemplateExpansion) error {
	ec.lock.Lock()
	defer ec.lock.Unlock()

	k := template.ObjectMeta.Name
	if k == "" {
		return fmt.Errorf("cannot upsert template with empty name")
	}

	ec.templates[k] = template.DeepCopy()
	return nil
}

func (ec *ExpansionCache) RemoveTemplate(template *mutationsunversioned.TemplateExpansion) error {
	ec.lock.Lock()
	defer ec.lock.Unlock()

	k := template.ObjectMeta.Name
	if k == "" {
		return fmt.Errorf("cannot remove template with empty name")
	}

	delete(ec.templates, k)
	return nil
}

func (ec *ExpansionCache) UpsertMutator(mut types.Mutator) error {
	if !mut.ExpandsGenerators() {
		return fmt.Errorf("cannot add mutator to cache that does not have 'origin: Generated' field")
	}

	ec.lock.Lock()
	defer ec.lock.Unlock()

	k := mut.ID()
	emptyID := types.ID{}
	if k == emptyID {
		return fmt.Errorf("cannot upsert mutator with empty ID")
	}

	ec.mutators[k] = mut
	return nil
}

func (ec *ExpansionCache) RemoveMutator(mut types.Mutator) error {
	ec.lock.Lock()
	defer ec.lock.Unlock()

	k := mut.ID()
	emptyID := types.ID{}
	if k == emptyID {
		return fmt.Errorf("cannot remove mutator with empty ID")
	}

	delete(ec.mutators, k)
	return nil
}

// MutatorsForGVK returns a slice of mutators that apply to specified GVK
func (ec *ExpansionCache) MutatorsForGVK(gvk schema.GroupVersionKind) []types.Mutator {
	ec.lock.Lock()
	defer ec.lock.Unlock()

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	mutable := &types.Mutable{Object: u}
	var muts []types.Mutator
	for _, mut := range ec.mutators {
		if mut.Matches(mutable) {
			muts = append(muts, mut)
		}
	}

	return muts
}

// TemplatesForGVK returns a slice of TemplateExpansions that match a given gvk
func (ec *ExpansionCache) TemplatesForGVK(gvk schema.GroupVersionKind) []mutationsunversioned.TemplateExpansion {
	ec.lock.Lock()
	defer ec.lock.Unlock()

	var templates []mutationsunversioned.TemplateExpansion
	for _, t := range ec.templates {
		for _, apply := range t.Spec.ApplyTo {
			if apply.Matches(gvk) {
				templates = append(templates, *t)
			}
		}
	}

	return templates
}

func NewExpansionCache(mutators []types.Mutator, templates []*mutationsunversioned.TemplateExpansion) (*ExpansionCache, error) {
	ec := &ExpansionCache{
		lock:      sync.Mutex{},
		mutators:  map[types.ID]types.Mutator{},
		templates: map[string]*mutationsunversioned.TemplateExpansion{},
	}

	if mutators != nil {
		for _, m := range mutators {
			if !m.ExpandsGenerators() {
				continue
			}
			if err := ec.UpsertMutator(m); err != nil {
				return nil, err
			}
		}
	}
	if templates != nil {
		for _, t := range templates {
			if err := ec.UpsertTemplate(t); err != nil {
				return nil, err
			}
		}
	}

	return ec, nil
}
