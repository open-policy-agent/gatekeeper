package mutation_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/google/go-cmp/cmp"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeID(t *testing.T) {
	config := &configv1alpha1.Config{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "Foo",
			Namespace: "Bar",
		},
	}

	// This is normally filled during the serialization
	gvk := schema.GroupVersionKind{
		Kind:    "kindname",
		Group:   "groupname",
		Version: "versionname",
	}
	config.APIVersion, config.Kind = gvk.ToAPIVersionAndKind()

	ID, err := mutation.MakeID(config)

	if err != nil {
		t.Errorf("MakeID failed %v", err)
	}

	expectedID := mutation.ID{
		Group:     "groupname",
		Kind:      "kindname",
		Name:      "Foo",
		Namespace: "Bar",
	}

	if !cmp.Equal(ID, expectedID) {
		t.Error("Generated ID not as expected", cmp.Diff(ID, expectedID))
	}
}
