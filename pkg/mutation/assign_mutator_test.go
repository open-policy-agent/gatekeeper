package mutation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	path "github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type assignTestCfg struct {
	value     runtime.RawExtension
	path      string
	pathTests []mutationsv1alpha1.PathTest
}

func makeValue(v interface{}) runtime.RawExtension {
	v2 := map[string]interface{}{
		"value": v,
	}
	j, err := json.Marshal(v2)
	if err != nil {
		panic(err)
	}
	return runtime.RawExtension{Raw: j}
}

func newAssignMutator(cfg *assignTestCfg) *AssignMutator {
	m := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name: "Foo",
		},
	}
	m.Spec.Parameters.Assign = cfg.value
	m.Spec.Location = cfg.path
	m.Spec.Parameters.PathTests = cfg.pathTests
	m2, err := MutatorForAssign(m)
	if err != nil {
		panic(err)
	}
	return m2
}

func newObj(value interface{}, path ...string) map[string]interface{} {
	root := map[string]interface{}{}
	current := root
	for _, node := range path {
		new := map[string]interface{}{}
		current[node] = new
		current = new
	}
	if err := unstructured.SetNestedField(root, value, path...); err != nil {
		panic(err)
	}
	return root
}

func newFoo(spec map[string]interface{}) *unstructured.Unstructured {
	data := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Foo",
		"metadata": map[string]interface{}{
			"name": "my-foo",
		},
	}
	if spec != nil {
		data["spec"] = spec
	}
	return &unstructured.Unstructured{Object: data}
}

func ensureObj(u *unstructured.Unstructured, expected interface{}, path ...string) error {
	v, exists, err := unstructured.NestedFieldNoCopy(u.Object, path...)
	if err != nil {
		return fmt.Errorf("could not retrieve value: %v", err)
	}
	if !exists {
		return fmt.Errorf("value does not exist at %+v: %s", path, spew.Sdump(u.Object))
	}
	if !reflect.DeepEqual(v, expected) {
		return fmt.Errorf("mutated value = %s, wanted %s", spew.Sdump(v), spew.Sdump(expected))
	}
	return nil
}

func ensureMissing(u *unstructured.Unstructured, path ...string) error {
	v, exists, err := unstructured.NestedFieldNoCopy(u.Object, path...)
	if err != nil {
		return fmt.Errorf("could not retrieve value: %v", err)
	}
	if exists {
		return fmt.Errorf("value exists at %+v as %v, expected missing: %s", path, v, spew.Sdump(u.Object))
	}
	return nil
}

func TestPathTests(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]interface{}
		cfg  *assignTestCfg
		fn   func(*unstructured.Unstructured) error
	}{
		{
			name: "no path test, missing val",
			spec: map[string]interface{}{},
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: nil,
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "hello", "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val present, missing val",
			spec: map[string]interface{}{},
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureMissing(u, "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val present, missing part of parent path",
			spec: newObj(map[string]interface{}{}, "please", "greet"),
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureMissing(u, "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val present, empty object as value",
			spec: newObj(map[string]interface{}{}, "please", "greet", "me"),
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "hello", "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val present, string as value",
			spec: newObj("never", "please", "greet", "me"),
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "hello", "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val missing, missing val",
			spec: map[string]interface{}{},
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "hello", "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val missing, missing val w/partial parent",
			spec: newObj(map[string]interface{}{}, "please", "greet"),
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "hello", "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val missing, empty object as value",
			spec: newObj(map[string]interface{}{}, "please", "greet", "me"),
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{}, "spec", "please", "greet", "me")
			},
		},
		{
			name: "expect val missing, string as value",
			spec: newObj("never", "please", "greet", "me"),
			cfg: &assignTestCfg{
				value:     makeValue("hello"),
				path:      "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "never", "spec", "please", "greet", "me")
			},
		},
		{
			name: "glob, sometimes match",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name": "c2",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:*].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name":           "c2",
						"securityPolicy": "made-by-mutation",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "glob, both match",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name": "c1",
				},
				map[string]interface{}{
					"name": "c2",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:*].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "made-by-mutation",
					},
					map[string]interface{}{
						"name":           "c2",
						"securityPolicy": "made-by-mutation",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "glob, sometimes match, MustExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name": "c2",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:*].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "made-by-mutation",
					},
					map[string]interface{}{
						"name": "c2",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "glob, both match, MustExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name":           "c2",
					"securityPolicy": "so-secure",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:*].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "made-by-mutation",
					},
					map[string]interface{}{
						"name":           "c2",
						"securityPolicy": "made-by-mutation",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "sidecar, MustNotExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name": "c2",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue(map[string]interface{}{"name": "sidecar"}),
				path:      "spec.containers[name:sidecar]",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name": "c2",
					},
					map[string]interface{}{
						"name": "sidecar",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "sidecar, noclobber, MustNotExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name": "c2",
				},
				map[string]interface{}{
					"name": "sidecar",
					"not":  "clobbered",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue(map[string]interface{}{"name": "sidecar"}),
				path:      "spec.containers[name:sidecar]",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name": "c2",
					},
					map[string]interface{}{
						"name": "sidecar",
						"not":  "clobbered",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "override container, MustExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name": "c2",
				},
				map[string]interface{}{
					"name":      "sidecar",
					"clobbered": "no",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue(map[string]interface{}{"name": "sidecar", "clobbered": "yes"}),
				path:      "spec.containers[name:sidecar]",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name": "c2",
					},
					map[string]interface{}{
						"name":      "sidecar",
						"clobbered": "yes",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "override container (missing), MustExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name": "c2",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue(map[string]interface{}{"name": "sidecar", "clobbered": "yes"}),
				path:      "spec.containers[name:sidecar]",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name": "c2",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "override specific subfield, MustExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name":           "c2",
					"securityPolicy": "so-secure",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:c2].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name":           "c2",
						"securityPolicy": "made-by-mutation",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "override specific subfield, MustNotExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name":           "c2",
					"securityPolicy": "so-secure",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:c2].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name":           "c2",
						"securityPolicy": "so-secure",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "override specific subfield, missing container",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
				map[string]interface{}{
					"name":           "c2",
					"securityPolicy": "so-secure",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:sidecar].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:sidecar].securityPolicy", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name":           "c2",
						"securityPolicy": "so-secure",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "override specific subfield (missing), MustExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:c2].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "override specific subfield (missing), MustNotExist",
			spec: newObj([]interface{}{
				map[string]interface{}{
					"name":           "c1",
					"securityPolicy": "so-secure",
				},
			}, "containers"),
			cfg: &assignTestCfg{
				value:     makeValue("made-by-mutation"),
				path:      "spec.containers[name:c2].securityPolicy",
				pathTests: []mutationsv1alpha1.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustNotExist}},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name":           "c1",
						"securityPolicy": "so-secure",
					},
					map[string]interface{}{
						"name":           "c2",
						"securityPolicy": "made-by-mutation",
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
		{
			name: "multitest, must + missing: case 1",
			spec: newObj(map[string]interface{}{}, "please", "greet"),
			cfg: &assignTestCfg{
				value: makeValue("hello"),
				path:  "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{
					{SubPath: "spec.please.greet", Condition: path.MustExist},
					{SubPath: "spec.please.greet.me", Condition: path.MustNotExist},
				},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "hello", "spec", "please", "greet", "me")
			},
		},
		{
			name: "multitest, must + missing: case 2",
			spec: newObj("never", "please", "greet", "me"),
			cfg: &assignTestCfg{
				value: makeValue("hello"),
				path:  "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{
					{SubPath: "spec.please.greet", Condition: path.MustExist},
					{SubPath: "spec.please.greet.me", Condition: path.MustNotExist},
				},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "never", "spec", "please", "greet", "me")
			},
		},
		{
			name: "multitest, must + missing: case 3",
			spec: newObj(map[string]interface{}{}, "please"),
			cfg: &assignTestCfg{
				value: makeValue("hello"),
				path:  "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{
					{SubPath: "spec.please.greet", Condition: path.MustExist},
					{SubPath: "spec.please.greet.me", Condition: path.MustNotExist},
				},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureMissing(u, "spec", "please", "greet")
			},
		},
		{
			name: "no partial mutation on failed test",
			spec: newObj(map[string]interface{}{}, "please"),
			cfg: &assignTestCfg{
				value: makeValue("hello"),
				path:  "spec.please.greet.me",
				pathTests: []mutationsv1alpha1.PathTest{
					{SubPath: "spec.please.greet", Condition: path.MustExist},
				},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureMissing(u, "spec", "please", "greet")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator := newAssignMutator(test.cfg)
			obj := newFoo(test.spec)
			err := mutator.Mutate(obj)
			if err != nil {
				t.Fatalf("failed mutation: %s", err)
			}
			if err := test.fn(obj); err != nil {
				t.Errorf("failed test: %v", err)
			}
		})
	}
}
