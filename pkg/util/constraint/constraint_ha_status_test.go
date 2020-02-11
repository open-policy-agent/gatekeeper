package constraint

import (
	"fmt"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHAStatus(t *testing.T) {
	tc := []struct {
		Name string
		// One status per pretend Pod
		Statuses []string
	}{
		{
			Name:     "One Status",
			Statuses: []string{"one_status"},
		},
		{
			Name:     "Two Statuses",
			Statuses: []string{"first", "second"},
		},
		{
			Name:     "Three Statuses",
			Statuses: []string{"first", "second", "third"},
		},
	}
	for tn, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			obj.Object = make(map[string]interface{})
			objGen := int64(tn)
			obj.SetGeneration(objGen)
			for i, s := range tt.Statuses {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				st, _ := GetHAStatus(obj)
				st.Errors = []Error{{Message: s}}
				if err := SetHAStatus(obj, st); err != nil {
					t.Fatal(err)
				}
				st2, _ := GetHAStatus(obj)
				if st2.ID != pod {
					t.Errorf("id = %v; want %v", st2.ID, pod)
				}
				if st2.ObservedGeneration != objGen {
					t.Errorf("observedGeneration = %v; want %v", st2.ObservedGeneration, objGen)
				}
				if st2.Errors[0].Message != s {
					t.Errorf("f = %v; wanted %v", st2.Errors[0].Message, s)
				}
			}
			statuses, exists, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
			if err != nil {
				t.Errorf("error while getting byPod: %v", err)
			}
			if !exists {
				t.Errorf("byPod does not exist")
			}
			if len(statuses) != len(tt.Statuses) {
				t.Errorf("len(statuses) = %d; want %d", len(statuses), len(tt.Statuses))
			}
			// Check for no overwrites
			for i, s := range tt.Statuses {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				st2, _ := GetHAStatus(obj)
				if st2.ID != pod {
					t.Errorf("t2: id = %v; want %v", st2.ID, pod)
				}
				if st2.ObservedGeneration != objGen {
					t.Errorf("observedGeneration = %v; want %v", st2.ObservedGeneration, objGen)
				}
				if st2.Errors[0].Message != s {
					t.Errorf("t2: f = %v; wanted %v", st2.Errors[0].Message, s)
				}
			}
			// Check deletion
			for i := range tt.Statuses {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				if err := DeleteHAStatus(obj); err != nil {
					t.Errorf("could not delete status: %s", err)
				}
				statuses, exists, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
				if err != nil {
					t.Errorf("error while getting byPod: %v", err)
				}
				if !exists {
					t.Errorf("byPod does not exist")
				}
				expected := len(tt.Statuses) - i - 1
				if len(statuses) != expected {
					t.Errorf("len(statuses) = %d; want %d", len(statuses), expected)
				}
			}
		})
	}
}
