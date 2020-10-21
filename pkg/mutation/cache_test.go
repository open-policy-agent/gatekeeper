package mutation_test

import (
	"testing"

	configv1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	corev1 "k8s.io/api/core/v1"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Leveraging existing resource types to create custom mutators in order to validate
// the cache.
type PodMutator struct {
	corev1.Pod
}

func (p *PodMutator) Mutate(obj runtime.Object) (runtime.Object, error) {
	return nil, nil
}

func (p *PodMutator) Matches(scheme *runtime.Scheme, obj runtime.Object, ns *corev1.Namespace) (bool, error) {
	return false, nil
}

func (p *PodMutator) Obj() runtime.Object {
	return &p.Pod
}

type ConfigMutator struct {
	configv1.Config
}

func (p *ConfigMutator) Mutate(obj runtime.Object) (runtime.Object, error) {
	return nil, nil
}

func (p *ConfigMutator) Matches(scheme *runtime.Scheme, obj runtime.Object, ns *corev1.Namespace) (bool, error) {
	return false, nil
}

func (p *ConfigMutator) Obj() runtime.Object {
	return &p.Config
}

var objects = []mutation.Mutator{
	&PodMutator{Pod: corev1.Pod{ObjectMeta: v1.ObjectMeta{Name: "bb", Namespace: "bar2"}}},
	&PodMutator{Pod: corev1.Pod{ObjectMeta: v1.ObjectMeta{Name: "aa", Namespace: "bar"}}},
	&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "dd", Namespace: "bb"}}},
	&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "cc", Namespace: "bb"}}},
	&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "cc", Namespace: "aa"}}},
}

var sorted = []mutation.Mutator{
	&PodMutator{Pod: corev1.Pod{ObjectMeta: v1.ObjectMeta{Name: "aa", Namespace: "bar"}}},
	&PodMutator{Pod: corev1.Pod{ObjectMeta: v1.ObjectMeta{Name: "bb", Namespace: "bar2"}}},
	&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "cc", Namespace: "aa"}}},
	&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "cc", Namespace: "bb"}}},
	&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "dd", Namespace: "bb"}}},
}

var testScheme = runtime.NewScheme()

func init() {
	corev1.AddToScheme(testScheme)
	configv1.AddToScheme(testScheme)
}

func TestSorting(t *testing.T) {
	cache := mutation.NewCache(testScheme)

	for i, obj := range objects {
		err := cache.Insert(obj)
		if err != nil {
			t.Errorf("Failed inserting %dth object", i)
		}
	}

	iterated := make([]mutation.Mutator, 0)
	cache.Iterate(func(m mutation.Mutator) error {
		iterated = append(iterated, m)
		return nil
	})

	if len(sorted) != len(objects) {
		t.Errorf("Expected %d object from the operator, found %d", len(sorted), len(objects))
	}

	if !cmp.Equal(sorted, iterated) {
		t.Errorf("Iteration not sorted: %s", cmp.Diff(sorted, iterated))
	}
}

func TestRemove(t *testing.T) {
	cache := mutation.NewCache(testScheme)

	for i, obj := range objects {
		err := cache.Insert(obj)
		if err != nil {
			t.Errorf("Failed inserting %dth object", i)
		}
	}

	cache.Remove(&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "dd", Namespace: "bb"}}})
	iterated := make([]mutation.Mutator, 0)
	cache.Iterate(func(m mutation.Mutator) error {
		iterated = append(iterated, m)
		return nil
	})

	expected := []mutation.Mutator{
		&PodMutator{Pod: corev1.Pod{ObjectMeta: v1.ObjectMeta{Name: "aa", Namespace: "bar"}}},
		&PodMutator{Pod: corev1.Pod{ObjectMeta: v1.ObjectMeta{Name: "bb", Namespace: "bar2"}}},
		&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "cc", Namespace: "aa"}}},
		&ConfigMutator{Config: configv1.Config{ObjectMeta: v1.ObjectMeta{Name: "cc", Namespace: "bb"}}},
	}

	if len(iterated) != len(expected) {
		t.Errorf("Expected %d object from the operator, found %d", len(iterated), len(expected))
	}

	if !cmp.Equal(expected, iterated) {
		t.Errorf("Cache content is not consistent: %s", cmp.Diff(expected, iterated))
	}
}
