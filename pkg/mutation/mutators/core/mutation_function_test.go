package core_test

import (
	"encoding/json"
	"testing"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/testhelpers"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	TestValue = "testValue"
)

func makeValue(v interface{}) mutationsunversioned.AssignField {
	return mutationsunversioned.AssignField{Value: &types.Anything{Value: v}}
}

func prepareTestPod(t *testing.T) *unstructured.Unstructured {
	pod := fakes.Pod(
		fakes.WithNamespace("foo"),
		fakes.WithName("test-pod"),
		fakes.WithLabels(map[string]string{"a": "b"}),
	)

	pod.Spec = corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "testname1",
				Image: "image",
				Ports: []corev1.ContainerPort{{Name: "portName1"}},
			},
			{
				Name:  "testname2",
				Image: "image",
				Ports: []corev1.ContainerPort{
					{Name: "portName2A"},
					{Name: "portName2B"},
				},
			},
			{
				Name:  "testname3",
				Image: "image",
				Ports: []corev1.ContainerPort{{Name: "portName3"}},
			},
		},
	}

	podObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		t.Errorf("Failed to convert pod to unstructured %v", err)
	}
	return &unstructured.Unstructured{Object: podObject}
}

func prepareTestPodWithPlaceholder(t *testing.T) *unstructured.Unstructured {
	pod := prepareTestPod(t)
	containers, _, err := unstructured.NestedSlice(pod.Object, "spec", "containers")
	if err != nil {
		t.Error("Unexpected error", err)
	}

	container, ok := containers[0].(map[string]interface{})
	if !ok {
		t.Error("Unable to cast container to map[string]interface{}")
	}

	container["image"] = &mutationsunversioned.ExternalDataPlaceholder{
		Ref: &mutationsunversioned.ExternalData{
			Provider: "old-provider",
		},
		ValueAtLocation: "old-image",
	}

	spec, ok := pod.Object["spec"].(map[string]interface{})
	if !ok {
		t.Error("Unable to cast spec to map[string]interface{}")
	}
	spec["containers"] = containers
	return pod
}

func TestObjects(t *testing.T) {
	testFunc := func(unstr *unstructured.Unstructured) {
		element, found, err := unstructured.NestedString(unstr.Object, "metadata", "labels", "labelA")
		if err != nil {
			t.Error("Unexpected error", err)
		}
		if !found && element != TestValue {
			t.Errorf("Failed to update pod")
		}
	}
	if err := testAssignMetadataMutation(
		"metadata.labels.labelA",
		TestValue,
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestObjectsAndLists(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		containers, _, err := unstructured.NestedSlice(u.Object, "spec", "containers")
		if err != nil {
			t.Error("Unexpected error", err)
		}
		for _, container := range containers {
			containerAsMap, ok := container.(map[string]interface{})
			if !ok {
				t.Fatalf("got container type %T, want %T", container, map[string]interface{}{})
			}

			ports, _, err := unstructured.NestedSlice(containerAsMap, "ports")
			if err != nil {
				t.Fatalf("getting ports: %v", err)
			}

			if containerAsMap["name"] == "testname2" {
				for _, port := range ports {
					portAsMap, ok := port.(map[string]interface{})
					if !ok {
						t.Fatalf("got port type %T, want %T", container, map[string]interface{}{})
					}

					if portAsMap["name"] == "portName2B" {
						if portAsMap["hostIP"] != TestValue {
							t.Errorf("Failed to update pod")
						}
					} else {
						if _, ok := portAsMap["hostIP"]; ok {
							t.Errorf("Unexpected pod was updated")
						}
					}
				}
			} else {
				for _, port := range ports {
					portAsMap, ok := port.(map[string]interface{})
					if !ok {
						t.Fatalf("got port type %T, want %T", container, map[string]interface{}{})
					}

					if _, ok := portAsMap["hostIP"]; ok {
						t.Errorf("Unexpected pod was updated")
					}
				}
			}
		}
	}

	if err := testAssignMutation(
		"", "v1", "Pod",
		`spec.containers["name": "testname2"].ports["name": "portName2B"].hostIP`,
		TestValue,
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestListsAsLastElementWithStringValue(t *testing.T) {
	testFunc := func(_ *unstructured.Unstructured) {}

	if err := testDummyMutation(
		`spec.containers["name": "notExists"]`,
		TestValue,
		prepareTestPod(t),
		testFunc,
		t,
	); err == nil {
		t.Errorf("List path entry in last position should accept a string value")
	}
}

func TestListsAsLastElement(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		containers, _, err := unstructured.NestedSlice(u.Object, "spec", "containers")
		if err != nil {
			t.Fatal("getting spec.containers", err)
		}
		for _, container := range containers {
			containerAsMap, ok := container.(map[string]interface{})
			if !ok {
				t.Fatalf("got container type %T, want %T", container, map[string]interface{}{})
			}

			if containerAsMap["name"] == "notExists" {
				if containerAsMap["foo"] == "foovalue" {
					return
				}
			}
		}
		t.Errorf("Failed to update pod. Container element was not added")
	}

	if err := testAssignMutation(
		"", "v1", "Pod",
		`spec.containers["name": "notExists"]`,
		map[string]interface{}{
			"name": "notExists",
			"foo":  "foovalue",
		},
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestListsAsLastElementAlreadyExists(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		containers, _, err := unstructured.NestedSlice(u.Object, "spec", "containers")
		if err != nil {
			t.Fatal("getting spec.containers", err)
		}
		for _, container := range containers {
			containerAsMap, ok := container.(map[string]interface{})
			if !ok {
				t.Fatalf("got container type %T, want %T", container, map[string]interface{}{})
			}

			if containerAsMap["name"] == "testname1" {
				if containerAsMap["foo"] == "bar" {
					return
				}
			}
		}
		t.Errorf("Pod's container was not replaced as expected")
	}

	if err := testAssignMutation(
		"", "v1", "Pod",
		`spec.containers["name": "testname1"]`,
		map[string]interface{}{
			"name": "testname1",
			"foo":  "bar",
		},
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestGlobbedList(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		containers, _, err := unstructured.NestedSlice(u.Object, "spec", "containers")
		if err != nil {
			t.Fatal("getting spec.containers", err)
		}
		for _, container := range containers {
			containerAsMap, ok := container.(map[string]interface{})
			if !ok {
				t.Fatalf("got container type %T, want %T", container, map[string]interface{}{})
			}

			ports, _, err := unstructured.NestedSlice(containerAsMap, "ports")
			if err != nil {
				t.Fatal("getting spec.containers[].ports")
			}
			for _, port := range ports {
				portAsMap, ok := port.(map[string]interface{})
				if !ok {
					t.Fatalf("got port type %T, want %T", port, map[string]interface{}{})
				}

				if value, ok := portAsMap["protocol"]; !ok || value != TestValue {
					t.Errorf("Expected value was not updated: %v, wanted %v", value, TestValue)
				}
			}
		}
	}

	if err := testAssignMutation(
		"", "v1", "Pod",
		`spec.containers["name": *].ports["name": *].protocol`,
		TestValue,
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestNonExistingPathEntry(t *testing.T) {
	testFunc := func(unstr *unstructured.Unstructured) {
		element, found, err := unstructured.NestedString(unstr.Object, "spec", "element", "should", "be", "added")
		if err != nil {
			t.Error("Unexpected error", err)
		}
		if !found && element != TestValue {
			t.Errorf("Failed to update pod")
		}
	}
	if err := testAssignMutation(
		"", "v1", "Pod",
		"spec.element.should.be.added",
		TestValue,
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestNonExistingListPathEntry(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		element, found, err := unstructured.NestedSlice(u.Object, "spec", "element")
		if err != nil {
			t.Error("Unexpected error", err)
		}
		if !found {
			t.Fatal("resource not found")
		}

		element0, ok := element[0].(map[string]interface{})
		if !ok {
			t.Fatalf("got spec.element[0] type %T, want %T", element[0], map[string]interface{}{})
		}

		element2, ok := element0["element2"].(map[string]interface{})
		if !ok {
			t.Fatalf("got spec.element[0].element2 type %T, want %T", element0["element2"], map[string]interface{}{})
		}
		if element2["added"] != TestValue {
			t.Errorf("Failed to update pod")
		}
	}
	if err := testAssignMutation(
		"", "v1", "Pod",
		`spec.element["name": "value"].element2.added`,
		TestValue,
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestAssignMetadataDoesNotUpdateExistingLabel(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		element, _, err := unstructured.NestedString(u.Object, "metadata", "labels", "a")
		if err != nil {
			t.Error("Unexpected error", err)
		}
		if element != "b" {
			t.Errorf("Value should not be updated")
		}
	}
	if err := testAssignMetadataMutation(
		`metadata.labels.a`,
		TestValue,
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestAssignDoesNotMatchObjectStructure(t *testing.T) {
	if err := testAssignMutation("", "v1", "Pod", `spec.containers.ports.protocol`, TestValue, prepareTestPod(t), nil, t); err == nil {
		t.Errorf("Error should be returned for mismatched path and object structure")
	}
}

func TestListsAsLastElementAlreadyExistsWithKeyConflict(t *testing.T) {
	testFunc := func(_ *unstructured.Unstructured) {}
	var v interface{}
	err := json.Unmarshal([]byte("{\"name\": \"conflictingName\", \"foo\": \"bar\"}"), &v)
	if err != nil {
		panic(err)
	}
	if err := testDummyMutation(
		`spec.containers["name": "testname1"]`,
		v,
		prepareTestPod(t),
		testFunc,
		t,
	); err == nil {
		t.Errorf("Expected error not raised. Conflicting name must not be applied.")
	} else if err.Error() != "key value of replaced object must not change" {
		t.Errorf("Incorrect error message: %s", err.Error())
	}
}

func TestIncomingPlaceholder(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		spec, ok := u.Object["spec"].(map[string]interface{})
		if !ok {
			t.Errorf("Unable to cast spec to map[string]interface{}")
		}

		containers, ok := spec["containers"].([]interface{})
		if !ok {
			t.Errorf("Unable to cast containers to []interface{}")
		}

		container, ok := containers[0].(map[string]interface{})
		if !ok {
			t.Errorf("Unable to cast container to map[string]interface{}")
		}

		placeholder, ok := container["image"].(*mutationsunversioned.ExternalDataPlaceholder)
		if !ok {
			t.Errorf("Unable to cast image to *mutationsunversioned.ExternalDataPlaceholder")
		}

		if placeholder.ValueAtLocation != "image" {
			t.Errorf("Expected placeholder's value at location to be 'image', got %s", placeholder.ValueAtLocation)
		}
	}
	placeholder := &mutationsunversioned.ExternalDataPlaceholder{
		Ref: &mutationsunversioned.ExternalData{
			Provider: "some-provider",
		},
	}
	if err := testDummyMutation(
		`spec.containers[name:testname1].image`,
		placeholder,
		prepareTestPod(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestPlaceholderWithIncomingPlaceholder(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		spec, ok := u.Object["spec"].(map[string]interface{})
		if !ok {
			t.Errorf("Unable to cast spec to map[string]interface{}")
		}

		containers, ok := spec["containers"].([]interface{})
		if !ok {
			t.Errorf("Unable to cast containers to []interface{}")
		}

		container, ok := containers[0].(map[string]interface{})
		if !ok {
			t.Errorf("Unable to cast container to map[string]interface{}")
		}

		placeholder, ok := container["image"].(*mutationsunversioned.ExternalDataPlaceholder)
		if !ok {
			t.Errorf("Unable to cast image to *mutationsunversioned.ExternalDataPlaceholder")
		}

		if placeholder.Ref.Provider != "new-provider" {
			t.Errorf("Expected placeholder's provider to be 'new-provider', got %s", placeholder.Ref.Provider)
		}
		if placeholder.ValueAtLocation != "old-image" {
			t.Errorf("Expected placeholder's value at location to be 'old-image', got %s", placeholder.ValueAtLocation)
		}
	}
	placeholder := &mutationsunversioned.ExternalDataPlaceholder{
		Ref: &mutationsunversioned.ExternalData{
			Provider: "new-provider",
		},
	}
	if err := testDummyMutation(
		`spec.containers[name:testname1].image`,
		placeholder,
		prepareTestPodWithPlaceholder(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func TestPlaceholderWithIncomingValue(t *testing.T) {
	testFunc := func(u *unstructured.Unstructured) {
		spec, ok := u.Object["spec"].(map[string]interface{})
		if !ok {
			t.Errorf("Unable to cast spec to map[string]interface{}")
		}

		containers, ok := spec["containers"].([]interface{})
		if !ok {
			t.Errorf("Unable to cast containers to []interface{}")
		}

		container, ok := containers[0].(map[string]interface{})
		if !ok {
			t.Errorf("Unable to cast container to map[string]interface{}")
		}

		if container["image"] != "new-image" {
			t.Errorf("Expected container's image to be 'new-image', got %s", container["image"])
		}
	}
	if err := testDummyMutation(
		`spec.containers[name:testname1].image`,
		"new-image",
		prepareTestPodWithPlaceholder(t),
		testFunc,
		t,
	); err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
}

func testDummyMutation(
	location string,
	value interface{},
	unstructured *unstructured.Unstructured,
	testFunc func(*unstructured.Unstructured),
	t *testing.T,
) error {
	mutator := testhelpers.NewDummyMutator("dummy", location, value)
	return testMutation(mutator, unstructured, testFunc, t)
}

func testAssignMutation(
	group, version, kind string,
	location string,
	value interface{},
	unstructured *unstructured.Unstructured,
	testFunc func(*unstructured.Unstructured),
	t *testing.T,
) error {
	assign := mutationsunversioned.Assign{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: mutationsunversioned.AssignSpec{
			ApplyTo:  []match.MutationApplyTo{{ApplyTo: match.ApplyTo{Groups: []string{group}, Versions: []string{version}, Kinds: []string{kind}}}},
			Location: location,
			Parameters: mutationsunversioned.Parameters{
				Assign: makeValue(value),
			},
		},
	}
	mutator, err := mutators.MutatorForAssign(&assign)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}
	return testMutation(mutator, unstructured, testFunc, t)
}

func testAssignMetadataMutation(
	location string,
	value string,
	unstructured *unstructured.Unstructured,
	testFunc func(*unstructured.Unstructured),
	t *testing.T,
) error {
	assignMetadata := mutationsunversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: mutationsunversioned.AssignMetadataSpec{
			Location: location,
			Parameters: mutationsunversioned.MetadataParameters{
				Assign: makeValue(value),
			},
		},
	}
	mutator, err := mutators.MutatorForAssignMetadata(&assignMetadata)
	if err != nil {
		t.Fatal("Unexpected error", err)
	}
	return testMutation(mutator, unstructured, testFunc, t)
}

func testMutation(mutator types.Mutator, unstructured *unstructured.Unstructured, testFunc func(*unstructured.Unstructured), _ *testing.T) error {
	_, err := mutator.Mutate(&types.Mutable{Object: unstructured})
	if err != nil {
		return err
	}
	testFunc(unstructured)
	return nil
}
