package constraint

import (
	"fmt"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestUnstructuredHAStatus(t *testing.T) {
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
				st, _ := getHAStatus(obj)
				st["someField"] = s
				if err := setHAStatus(obj, st); err != nil {
					t.Fatal(err)
				}
				st2, _ := getHAStatus(obj)
				id2, ok := st2["id"]
				if !ok {
					t.Errorf("id not set for host %d", i)
				}
				id, ok := id2.(string)
				if !ok {
					t.Errorf("id (%v) is not a string for host %d", id2, i)
				}
				if id != pod {
					t.Errorf("id = %v; want %v", id, pod)
				}
				gen2, ok := st2["observedGeneration"]
				if !ok {
					t.Errorf("observedGeneration not set for host %d", i)
				}
				gen, ok := gen2.(int64)
				if !ok {
					t.Errorf("observedGeneration (%v) is not an integer for host %d", gen, i)
				}
				if gen != objGen {
					t.Errorf("observedGeneration = %v; want %v", gen, objGen)
				}
				f2, ok := st2["someField"]
				if !ok {
					t.Errorf("field not set for host %d", i)
				}
				f, ok := f2.(string)
				if !ok {
					t.Errorf("f (%v) is not a string for host %d", f2, i)
				}
				if f != s {
					t.Errorf("f = %v; wanted %v", f, s)
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
				st2, _ := getHAStatus(obj)
				id2, ok := st2["id"]
				if !ok {
					t.Errorf("t2: id not set for host %d", i)
				}
				id, ok := id2.(string)
				if !ok {
					t.Errorf("t2: id (%v) is not a string for host %d", id2, i)
				}
				if id != pod {
					t.Errorf("t2: id = %v; want %v", id, pod)
				}
				gen2, ok := st2["observedGeneration"]
				if !ok {
					t.Errorf("observedGeneration not set for host %d", i)
				}
				gen, ok := gen2.(int64)
				if !ok {
					t.Errorf("observedGeneration (%v) is not an integer for host %d", gen, i)
				}
				if gen != objGen {
					t.Errorf("observedGeneration = %v; want %v", gen, objGen)
				}
				f2, ok := st2["someField"]
				if !ok {
					t.Errorf("t2: field not set for host %d", i)
				}
				f, ok := f2.(string)
				if !ok {
					t.Errorf("t2: f (%v) is not a string for host %d", f2, i)
				}
				if f != s {
					t.Errorf("t2: f = %v; wanted %v", f, s)
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
