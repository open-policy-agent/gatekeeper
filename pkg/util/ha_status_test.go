package util

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/api/v1alpha1"
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
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			obj.Object = make(map[string]interface{})
			for i, s := range tt.Statuses {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				st, _ := GetHAStatus(obj)
				st["someField"] = s
				if err := SetHAStatus(obj, st); err != nil {
					t.Fatal(err)
				}
				st2, _ := GetHAStatus(obj)
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
				st2, _ := GetHAStatus(obj)
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
		})
	}
}

func TestCfgAStatus(t *testing.T) {
	tc := []struct {
		Name string
		// One kind per pretend Pod
		Kinds []configv1alpha1.GVK
	}{
		{
			Name:  "One Status",
			Kinds: []configv1alpha1.GVK{{Kind: "one_status"}},
		},
		{
			Name:  "Two Statuses",
			Kinds: []configv1alpha1.GVK{{Kind: "one"}, {Kind: "two"}},
		},
		{
			Name:  "Three Statuses",
			Kinds: []configv1alpha1.GVK{{Kind: "one"}, {Kind: "two"}, {Kind: "three"}},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			obj := &configv1alpha1.Config{}
			for i, k := range tt.Kinds {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				st := GetCfgHAStatus(obj)
				ks := []configv1alpha1.GVK{k}
				st.AllFinalizers = ks
				SetCfgHAStatus(obj, st)
				st2 := GetCfgHAStatus(obj)
				if st2.ID != pod {
					t.Errorf("id = %v; want %v", st2.ID, pod)
				}
				if !reflect.DeepEqual(st2.AllFinalizers, ks) {
					t.Errorf("st2.AllFinalizers = %v; wanted %v", st2.AllFinalizers, ks)
				}
			}
			if len(obj.Status.ByPod) != len(tt.Kinds) {
				t.Errorf("len(obj.Status.ByPod) = %d; want %d", len(obj.Status.ByPod), len(tt.Kinds))
			}
			// Check for no overwrites
			for i, k := range tt.Kinds {
				pod := fmt.Sprintf("Pod%d", i)
				if err := os.Setenv("POD_NAME", pod); err != nil {
					t.Fatal(err)
				}
				ks := []configv1alpha1.GVK{k}
				st2 := GetCfgHAStatus(obj)
				if st2.ID != pod {
					t.Errorf("t2: id = %v; want %v", st2.ID, pod)
				}
				if !reflect.DeepEqual(st2.AllFinalizers, ks) {
					t.Errorf("t2: st2.AllFinalizers = %v; wanted %v", st2.AllFinalizers, ks)
				}
			}
		})
	}
}

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
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			obj := &v1beta1.ConstraintTemplate{}
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
				if !reflect.DeepEqual(st2.Errors, es) {
					t.Errorf("t2: st2.Errors = %v; wanted %v", st2.Errors, es)
				}
			}
		})
	}
}
