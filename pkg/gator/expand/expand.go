package expand

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/v3/apis/expansion/unversioned"
	expansionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1alpha1"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assignimage"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/modifyset"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var mutatorKinds = map[string]bool{
	"Assign":         true,
	"AssignMetadata": true,
	"ModifySet":      true,
	"AssignImage":    true,
}

type Expander struct {
	mutators           []types.Mutator
	templateExpansions []*unversioned.ExpansionTemplate
	objects            []*unstructured.Unstructured
	namespaces         map[string]*corev1.Namespace
	expSystem          *expansion.System
	mutSystem          *mutation.System
}

func Expand(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	er, err := NewExpander(resources)
	if err != nil {
		return nil, err
	}

	var resultantObjs []*unstructured.Unstructured
	for _, obj := range er.objects {
		resultants, err := er.Expand(obj)
		if err != nil {
			return nil, err
		}
		for _, r := range resultants {
			resultantObjs = append(resultantObjs, r.Obj)
		}
	}

	return resultantObjs, nil
}

func NewExpander(resources []*unstructured.Unstructured) (*Expander, error) {
	mutSystem := mutation.NewSystem(mutation.SystemOpts{})
	er := &Expander{
		mutSystem:  mutSystem,
		expSystem:  expansion.NewSystem(mutSystem),
		namespaces: make(map[string]*corev1.Namespace),
	}

	if err := er.addResources(resources); err != nil {
		return nil, fmt.Errorf("error parsing resources: %w", err)
	}

	for _, te := range er.templateExpansions {
		if err := er.expSystem.UpsertTemplate(te); err != nil {
			return nil, fmt.Errorf("error upserting template %s: %w", te.Name, err)
		}
	}

	for _, m := range er.mutators {
		if err := er.mutSystem.Upsert(m); err != nil {
			return nil, fmt.Errorf("error upserting mutator: %w", err)
		}
	}

	return er, nil
}

func (er *Expander) Expand(resource *unstructured.Unstructured) ([]*expansion.Resultant, error) {
	ns, _ := er.NamespaceForResource(resource)

	// Mutate the base resource before expanding it
	base := &types.Mutable{
		Object:    resource,
		Namespace: ns,
		Username:  "",
		Source:    types.SourceTypeOriginal,
	}
	if _, err := er.mutSystem.Mutate(context.Background(), base); err != nil {
		return nil, fmt.Errorf("error mutating base resource %s: %w", resource.GetName(), err)
	}

	resultants, err := er.expSystem.Expand(base)
	if err != nil {
		return nil, fmt.Errorf("error expanding resource %s: %w", resource.GetName(), err)
	}

	return resultants, nil
}

func (er *Expander) NamespaceForResource(r *unstructured.Unstructured) (*corev1.Namespace, bool) {
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

func (er *Expander) addResources(resources []*unstructured.Unstructured) error {
	for _, r := range resources {
		if err := er.add(r); err != nil {
			return err
		}
	}
	return nil
}

func (er *Expander) addMutator(mut *unstructured.Unstructured) error {
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
	case "AssignImage":
		a, err := convertAssignImage(mut)
		if err != nil {
			return err
		}
		m, mutErr = assignimage.MutatorForAssignImage(a)
	default:
		return fmt.Errorf("cannot convert mutator of kind %q", mut.GetKind())
	}

	if mutErr != nil {
		return mutErr
	}
	er.mutators = append(er.mutators, m)
	return nil
}

func (er *Expander) add(u *unstructured.Unstructured) error {
	var err error
	switch {
	case isMutator(u):
		err = er.addMutator(u)
	case isExpansion(u):
		err = er.addExpansionTemplate(u)
	case isNamespace(u):
		err = er.addNamespace(u)
	}
	if err == nil {
		// Any resource can technically be a generator
		er.objects = append(er.objects, u)
	}

	return err
}

func (er *Expander) addExpansionTemplate(u *unstructured.Unstructured) error {
	te, err := convertExpansionTemplate(u)
	if err != nil {
		return err
	}
	er.templateExpansions = append(er.templateExpansions, te)
	return nil
}

func (er *Expander) addNamespace(u *unstructured.Unstructured) error {
	ns, err := convertNamespace(u)
	if err != nil {
		return err
	}
	er.namespaces[ns.GetName()] = ns
	return nil
}

func isExpansion(u *unstructured.Unstructured) bool {
	return u.GroupVersionKind().Group == expansionv1alpha1.GroupVersion.Group && u.GetKind() == "ExpansionTemplate"
}

func isMutator(obj *unstructured.Unstructured) bool {
	if _, exists := mutatorKinds[obj.GetKind()]; !exists {
		return false
	}
	return obj.GroupVersionKind().Group == mutationsv1alpha1.GroupVersion.Group
}

func isNamespace(obj *unstructured.Unstructured) bool {
	return obj.GetKind() == "Namespace" && obj.GroupVersionKind().Group == corev1.SchemeGroupVersion.Group
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

func convertAssignImage(u *unstructured.Unstructured) (*mutationsunversioned.AssignImage, error) {
	ai := &mutationsunversioned.AssignImage{}
	err := convertUnstructuredToTyped(u, ai)
	return ai, err
}

func convertNamespace(u *unstructured.Unstructured) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := convertUnstructuredToTyped(u, ns)
	return ns, err
}
