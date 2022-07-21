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

var (
	MutatorKinds = map[string]bool{
		"Assign":         true,
		"AssignMetadata": true,
		"ModifySet":      true,
	}
	MutatorAPIVersions = map[string]bool{
		"mutations.gatekeeper.sh/v1alpha1": true,
		"mutations.gatekeeper.sh/v1beta1":  true,
	}
	ExpansionAPIVersions = map[string]bool{
		"expansion.gatekeeper.sh/v1alpha1": true,
	}
)

type expansionResources struct {
	mutators           []types.Mutator
	templateExpansions []*unversioned.ExpansionTemplate
	generators         []*unstructured.Unstructured
	namespaces         map[string]*corev1.Namespace
}

func Expand(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	expSystem := expansion.NewSystem(mutation.NewSystem(mutation.SystemOpts{}))
	er := expansionResources{}
	if err := er.addResources(resources); err != nil {
		return nil, fmt.Errorf("error parsing resources: %s", err)
	}

	for _, te := range er.templateExpansions {
		if err := expSystem.UpsertTemplate(te); err != nil {
			return nil, err
		}
	}

	var resultants []*unstructured.Unstructured
	for _, gen := range er.generators {
		ns, err := er.namespaceForGenerator(gen)
		base := &types.Mutable{
			Object:    gen,
			Namespace: ns,
			Username:  "",
			Source:    types.SourceTypeGenerated,
		}
		if err != nil {
			return nil, fmt.Errorf("error expanding generator: %s", err)
		}
		r, err := expSystem.Expand(base)
		if err != nil {
			return nil, fmt.Errorf("error expanding generator: %s", err)
		}
		resultants = append(resultants, r...)
	}

	return resultants, nil
}

func (er *expansionResources) namespaceForGenerator(gen *unstructured.Unstructured) (*corev1.Namespace, error) {
	genNs := gen.GetNamespace()
	if genNs == "" {
		return &corev1.Namespace{}, nil
	}
	if genNs == "default" {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}, nil
	}

	ns, exists := er.namespaces[genNs]
	if !exists {
		return nil, fmt.Errorf("namespace resource %q not found in supplied configs", genNs)
	}
	return ns, nil
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
	case u.GetKind() == "Namespace":
		err = er.addNamespace(u)
	default:
		er.generators = append(er.generators, u)
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
	_, exists := ExpansionAPIVersions[u.GetAPIVersion()]
	return exists && u.GetKind() == "ExpansionTemplate"
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
