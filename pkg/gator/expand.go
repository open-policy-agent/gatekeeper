package gator

import (
	"fmt"

	"github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/modifyset"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var mutatorKinds = map[string]bool{
	"Assign":         true,
	"AssignMetadata": true,
	"ModifySet":      true,
}

const (
	mutatorGroup   = "mutations.gatekeeper.sh"
	expansionGroup = "expansion.gatekeeper.sh"
)

type expansionResources struct {
	mutators           []types.Mutator
	templateExpansions []*unversioned.ExpansionTemplate
	objects            []*unstructured.Unstructured
	namespaces         map[string]*corev1.Namespace
}

func Expand(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	mutSystem := mutation.NewSystem(mutation.SystemOpts{})
	expSystem := expansion.NewSystem(mutSystem)
	er := expansionResources{}
	if err := er.addResources(resources); err != nil {
		return nil, fmt.Errorf("error parsing resources: %s", err)
	}

	for _, te := range er.templateExpansions {
		if err := expSystem.UpsertTemplate(te); err != nil {
			return nil, fmt.Errorf("error upserting template %s: %s", te.Name, err)
		}
	}

	for _, m := range er.mutators {
		if err := mutSystem.Upsert(m); err != nil {
			return nil, fmt.Errorf("error upserting mutator: %s", err)
		}
	}

	var resultants []*unstructured.Unstructured
	for _, obj := range er.objects {
		ns, nsFound := er.namespaceForResource(obj)
		base := &types.Mutable{
			Object:    obj,
			Namespace: ns,
			Username:  "",
			Source:    types.SourceTypeOriginal,
		}

		// Mutate the resource before expanding
		if _, err := mutSystem.Mutate(base); err != nil {
			return nil, fmt.Errorf("error mutating base resource %s: %s", obj.GetName(), err)
		}

		r, err := expSystem.Expand(base)
		if err != nil {
			return nil, fmt.Errorf("error expanding resource %s: %s", obj.GetName(), err)
		}

		// If any resultant resources were created, we must ensure the namespace
		// for the base resource was supplied for Matching to work properly
		if len(r) > 0 && !nsFound {
			return nil, fmt.Errorf("no namespace config supplied for resource %s", obj.GetName())
		}

		resultants = append(resultants, r...)
	}

	return resultants, nil
}

func (er *expansionResources) namespaceForResource(r *unstructured.Unstructured) (*corev1.Namespace, bool) {
	rNs := r.GetNamespace()
	if rNs == "" {
		return &corev1.Namespace{}, true
	}

	ns, exists := er.namespaces[rNs]
	if !exists {
		if rNs == "default" {
			return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}, true
		}
		return nil, false
	}
	return ns, true
}

func (er *expansionResources) addResources(resources []*unstructured.Unstructured) error {
	for _, r := range resources {
		if err := er.add(r); err != nil {
			return err
		}
	}
	return nil
}

func (er *expansionResources) addMutator(mut *unstructured.Unstructured) error {
	var mutErr error
	var m types.Mutator

	switch mut.GetKind() {
	case "Assign":
		a, err := convertAssign(mut)
		if err != nil {
			return err
		}
		m, mutErr = assign.MutatorForAssign(a)
	case "AssignMetadata":
		a, err := convertAssignMetadata(mut)
		if err != nil {
			return err
		}
		m, mutErr = assignmeta.MutatorForAssignMetadata(a)
	case "ModifySet":
		ms, err := convertModifySet(mut)
		if err != nil {
			return err
		}
		m, mutErr = modifyset.MutatorForModifySet(ms)
	default:
		return fmt.Errorf("cannot convert mutator of kind %q", mut.GetKind())
	}

	if mutErr != nil {
		return mutErr
	}
	er.mutators = append(er.mutators, m)
	return nil
}

func (er *expansionResources) add(u *unstructured.Unstructured) error {
	var err error
	switch {
	case isMutator(u):
		err = er.addMutator(u)
	case isExpansion(u):
		err = er.addExpansionTemplate(u)
	case isNamespace(u):
		err = er.addNamespace(u)
	}

	if err != nil {
		// Any resource can technically be a generator
		er.objects = append(er.objects, u)
	}

	return err
}

func (er *expansionResources) addExpansionTemplate(u *unstructured.Unstructured) error {
	te, err := convertExpansionTemplate(u)
	if err != nil {
		return err
	}
	er.templateExpansions = append(er.templateExpansions, te)
	return nil
}

func (er *expansionResources) addNamespace(u *unstructured.Unstructured) error {
	ns, err := convertNamespace(u)
	if err != nil {
		return err
	}
	er.namespaces[ns.GetName()] = ns
	return nil
}

func isExpansion(u *unstructured.Unstructured) bool {
	return u.GroupVersionKind().Group == expansionGroup && u.GetKind() == "ExpansionTemplate"
}

func isMutator(obj *unstructured.Unstructured) bool {
	if _, exists := mutatorKinds[obj.GetKind()]; !exists {
		return false
	}
	return obj.GroupVersionKind().Group == mutatorGroup
}

func isNamespace(obj *unstructured.Unstructured) bool {
	return obj.GetKind() == "Namespace" && obj.GetAPIVersion() == "v1"
}

func convertUnstructuredToTyped(u *unstructured.Unstructured, obj interface{}) error {
	if u == nil {
		return fmt.Errorf("cannot convert nil unstructured to type")
	}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), obj)
	return err
}

func convertExpansionTemplate(u *unstructured.Unstructured) (*unversioned.ExpansionTemplate, error) {
	te := &unversioned.ExpansionTemplate{}
	err := convertUnstructuredToTyped(u, te)
	return te, err
}

func convertAssign(u *unstructured.Unstructured) (*mutationsunversioned.Assign, error) {
	a := &mutationsunversioned.Assign{}
	err := convertUnstructuredToTyped(u, a)
	return a, err
}

func convertAssignMetadata(u *unstructured.Unstructured) (*mutationsunversioned.AssignMetadata, error) {
	am := &mutationsunversioned.AssignMetadata{}
	err := convertUnstructuredToTyped(u, am)
	return am, err
}

func convertModifySet(u *unstructured.Unstructured) (*mutationsunversioned.ModifySet, error) {
	ms := &mutationsunversioned.ModifySet{}
	err := convertUnstructuredToTyped(u, ms)
	return ms, err
}

func convertNamespace(u *unstructured.Unstructured) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := convertUnstructuredToTyped(u, ns)
	return ns, err
}
