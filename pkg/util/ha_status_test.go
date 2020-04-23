package util

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
)

func TestCTHAStatus(t *testing.T) {
	tc := []struct {
		Name string
		// One error per pretend Pod
		Errors []*v1beta1.CreateCRDError
	}{
		{
			Name:   "One Status",
			Errors: []*v1beta1.CreateCRDError{{Message: "one_status"}},
		},
		{
			Name:   "Two Statuses",
			Errors: []*v1beta1.CreateCRDError{{Message: "one"}, {Message: "two"}},
		},
		{
			Name:   "Three Statuses",
			Errors: []*v1beta1.CreateCRDError{{Message: "one"}, {Message: "two"}, {Message: "three"}},
		},
	}
	for tn, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			obj := &v1beta1.ConstraintTemplate{}
			objGen := int64(tn)
			obj.SetGeneration(objGen)
			for i, e := range tt.Errors {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				st := GetCTHAStatus(obj)
				es := []*v1beta1.CreateCRDError{e}
				st.Errors = es
				SetCTHAStatus(obj, st)
				st2 := GetCTHAStatus(obj)
				if st2.ID != pod {
					t.Errorf("id = %v; want %v", st2.ID, pod)
				}
				if st2.ObservedGeneration != objGen {
					t.Errorf("t2: observedGeneration = %v; want %v", st2.ObservedGeneration, objGen)
				}
				if !reflect.DeepEqual(st2.Errors, es) {
					t.Errorf("st2.Errors = %v; wanted %v", st2.Errors, es)
				}
			}
			if len(obj.Status.ByPod) != len(tt.Errors) {
				t.Errorf("len(obj.Status.ByPod) = %d; want %d", len(obj.Status.ByPod), len(tt.Errors))
			}
			// Check for no overwrites
			for i, e := range tt.Errors {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				es := []*v1beta1.CreateCRDError{e}
				st2 := GetCTHAStatus(obj)
				if st2.ID != pod {
					t.Errorf("t2: id = %v; want %v", st2.ID, pod)
				}
				if st2.ObservedGeneration != objGen {
					t.Errorf("t2: observedGeneration = %v; want %v", st2.ObservedGeneration, objGen)
				}
				if !reflect.DeepEqual(st2.Errors, es) {
					t.Errorf("t2: st2.Errors = %v; wanted %v", st2.Errors, es)
				}
			}
			// Check deletion
			for i := range tt.Errors {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				DeleteCTHAStatus(obj)
				expected := len(tt.Errors) - i - 1
				if len(obj.Status.ByPod) != expected {
					t.Errorf("len(statuses) = %d; want %d", len(obj.Status.ByPod), expected)
				}
			}
		})
	}
}
