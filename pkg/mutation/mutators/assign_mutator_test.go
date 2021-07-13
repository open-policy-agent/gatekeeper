package mutators

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	path "github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type assignTestCfg struct {
	value     runtime.RawExtension
	path      string
	pathTests []mutationsv1alpha1.PathTest
	in        []interface{}
	notIn     []interface{}
	applyTo   []match.ApplyTo
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
	m.Spec.ApplyTo = cfg.applyTo
	vt := &mutationsv1alpha1.AssignIf{
		In:    cfg.in,
		NotIn: cfg.notIn,
	}
	bs, err := json.Marshal(vt)
	if err != nil {
		panic(err)
	}
	m.Spec.Parameters.AssignIf = runtime.RawExtension{Raw: bs}
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
		m := map[string]interface{}{}
		current[node] = m
		current = m
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

func newPod(pod *v1.Pod) *unstructured.Unstructured {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		panic(fmt.Sprintf("converting pod to unstructured: %v", err))
	}
	return &unstructured.Unstructured{Object: u}
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
		return fmt.Errorf("mutated value = \n%s\n\n, wanted \n%s\n\n, diff \n%s", spew.Sdump(v), spew.Sdump(expected), cmp.Diff(v, expected))
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo:   []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
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
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("hello"),
				path:    "spec.please.greet.me",
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
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("hello"),
				path:    "spec.please.greet.me",
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
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("hello"),
				path:    "spec.please.greet.me",
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
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("hello"),
				path:    "spec.please.greet.me",
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
			_, err := mutator.Mutate(obj)
			if err != nil {
				t.Fatalf("failed mutation: %s", err)
			}
			if err := test.fn(obj); err != nil {
				t.Errorf("failed test: %v", err)
			}
		})
	}
}

func TestValueTests(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]interface{}
		cfg  *assignTestCfg
		fn   func(*unstructured.Unstructured) error
	}{
		{
			name: "number, empty, mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(42), "spec", "hi")
			},
		},
		{
			name: "number, 1, in, mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(7)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(42), "spec", "hi")
			},
		},
		{
			name: "number, 2, in, mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(3), float64(7)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(42), "spec", "hi")
			},
		},
		{
			name: "number, 1, not in, mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				notIn:   []interface{}{float64(222)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(42), "spec", "hi")
			},
		},
		{
			name: "number, 2, not in, mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				notIn:   []interface{}{float64(3), float64(222)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(42), "spec", "hi")
			},
		},
		{
			name: "number, 1, in, no mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(27)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(7), "spec", "hi")
			},
		},
		{
			name: "number, 2, in, no mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(-345), float64(27)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(7), "spec", "hi")
			},
		},
		{
			name: "number, mixed, mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(-345), float64(7)},
				notIn:   []interface{}{float64(4), float64(2)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(42), "spec", "hi")
			},
		},
		{
			name: "number, mixed, no mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(-345), float64(27)},
				notIn:   []interface{}{float64(4), float64(2)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(7), "spec", "hi")
			},
		},
		{
			name: "number, overlap, no mutate",
			spec: map[string]interface{}{"hi": float64(7)},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(-345), float64(7)},
				notIn:   []interface{}{float64(4), float64(7)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(7), "spec", "hi")
			},
		},
		{
			name: "number, in, no value, no mutate",
			spec: map[string]interface{}{},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				in:      []interface{}{float64(-345), float64(7)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureMissing(u, "spec", "hi")
			},
		},
		{
			name: "number, not in, no value, mutate",
			spec: map[string]interface{}{},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(42),
				path:    "spec.hi",
				notIn:   []interface{}{float64(-345), float64(7)},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, float64(42), "spec", "hi")
			},
		},

		{
			name: "string, empty, mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "bye", "spec", "hi")
			},
		},
		{
			name: "string, 1, in, mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"there"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "bye", "spec", "hi")
			},
		},
		{
			name: "string, 2, in, mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"argh", "there"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "bye", "spec", "hi")
			},
		},
		{
			name: "string, 1, not in, mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				notIn:   []interface{}{"argh"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "bye", "spec", "hi")
			},
		},
		{
			name: "string, 2, not in, mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				notIn:   []interface{}{"cows", "only"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "bye", "spec", "hi")
			},
		},
		{
			name: "string, 1, in, no mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"super"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "there", "spec", "hi")
			},
		},
		{
			name: "string, 2, in, no mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"moo", "turkey"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "there", "spec", "hi")
			},
		},
		{
			name: "string, mixed, mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"honk", "there"},
				notIn:   []interface{}{"car", "almond"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "bye", "spec", "hi")
			},
		},
		{
			name: "string, mixed, no mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"rocket", "return"},
				notIn:   []interface{}{"word", "association"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "there", "spec", "hi")
			},
		},
		{
			name: "string, overlap, no mutate",
			spec: map[string]interface{}{"hi": "there"},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"over", "there"},
				notIn:   []interface{}{"not", "there"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "there", "spec", "hi")
			},
		},
		{
			name: "string, in, no value, no mutate",
			spec: map[string]interface{}{},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				in:      []interface{}{"strings are fun", "there"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureMissing(u, "spec", "hi")
			},
		},
		{
			name: "string, not in, no value, mutate",
			spec: map[string]interface{}{},
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("bye"),
				path:    "spec.hi",
				notIn:   []interface{}{"much stringage", "there"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "bye", "spec", "hi")
			},
		},

		{
			name: "empty object, in, mutate",
			spec: newObj(map[string]interface{}{}, "please"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(newObj("there", "mutate")),
				path:    "spec.please",
				// use the JSON parser to make sure we see empty objects as JSON does.
				in: func() []interface{} {
					var out []interface{}
					if err := json.Unmarshal([]byte(`[{}]`), &out); err != nil {
						panic(err)
					}
					return out
				}(),
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{"mutate": "there"}, "spec", "please")
			},
		},
		{
			name: "empty object, not in, no mutate",
			spec: newObj(map[string]interface{}{}, "please"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(newObj("there", "mutate")),
				path:    "spec.please",
				// use the JSON parser to make sure we see empty objects as JSON does.
				notIn: func() []interface{} {
					var out []interface{}
					if err := json.Unmarshal([]byte(`[{}]`), &out); err != nil {
						panic(err)
					}
					return out
				}(),
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{}, "spec", "please")
			},
		},
		{
			name: "trivial object, in, mutate",
			spec: newObj("here", "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(newObj("there", "mutate")),
				path:    "spec.please",
				in:      []interface{}{map[string]string{"mutate": "here"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{"mutate": "there"}, "spec", "please")
			},
		},
		{
			name: "trivial object, in, no mutate",
			spec: newObj("here", "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(newObj("there", "mutate")),
				path:    "spec.please",
				in:      []interface{}{map[string]string{"mutate": "never"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{"mutate": "here"}, "spec", "please")
			},
		},
		{
			name: "trivial object, not in, mutate",
			spec: newObj("here", "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(newObj("there", "mutate")),
				path:    "spec.please",
				notIn:   []interface{}{map[string]string{"mutate": "always"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{"mutate": "there"}, "spec", "please")
			},
		},
		{
			name: "trivial object, not in, no mutate",
			spec: newObj("here", "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue(newObj("there", "mutate")),
				path:    "spec.please",
				notIn:   []interface{}{map[string]string{"mutate": "here"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{"mutate": "here"}, "spec", "please")
			},
		},

		{
			name: "complex object, in, mutate",
			spec: newObj(map[string]interface{}{
				"aString": "yep",
				"anObject": map[string]interface{}{
					"also": "yes",
				},
			}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				in: []interface{}{map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "yes",
					},
				}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "replaced", "spec", "please", "mutate")
			},
		},
		{
			name: "complex object, in, no mutate",
			spec: newObj(map[string]interface{}{
				"aString": "yep",
				"anObject": map[string]interface{}{
					"also": "yes",
				},
			}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				in: []interface{}{map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "no",
					},
				}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "yes",
					},
				}, "spec", "please", "mutate")
			},
		},
		{
			name: "complex object, in, extra, no mutate",
			spec: newObj(map[string]interface{}{
				"aString": "yep",
				"anObject": map[string]interface{}{
					"also": "yes",
				},
			}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				in: []interface{}{map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "yes",
						"i":    "think",
					},
				}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "yes",
					},
				}, "spec", "please", "mutate")
			},
		},
		{
			name: "complex object, not in, mutate",
			spec: newObj(map[string]interface{}{
				"aString": "yep",
				"anObject": map[string]interface{}{
					"also": "yes",
				},
			}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				notIn: []interface{}{map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "no",
					},
				}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "replaced", "spec", "please", "mutate")
			},
		},
		{
			name: "complex object, not in, no mutate",
			spec: newObj(map[string]interface{}{
				"aString": "yep",
				"anObject": map[string]interface{}{
					"also": "yes",
				},
			}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				notIn: []interface{}{map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "yes",
					},
				}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, map[string]interface{}{
					"aString": "yep",
					"anObject": map[string]interface{}{
						"also": "yes",
					},
				}, "spec", "please", "mutate")
			},
		},

		{
			name: "empty list, in, mutate",
			spec: newObj([]interface{}{}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				in:      []interface{}{[]interface{}{}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "replaced", "spec", "please", "mutate")
			},
		},
		{
			name: "empty list, in, no mutate",
			spec: newObj([]interface{}{}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				in:      []interface{}{[]interface{}{"hey"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, []interface{}{}, "spec", "please", "mutate")
			},
		},
		{
			name: "empty list, not in, no mutate",
			spec: newObj([]interface{}{}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				notIn:   []interface{}{[]interface{}{}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, []interface{}{}, "spec", "please", "mutate")
			},
		},
		{
			name: "list, in, no mutate",
			spec: newObj([]interface{}{"one", "two"}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				in:      []interface{}{[]interface{}{"one", "two", "three"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, []interface{}{"one", "two"}, "spec", "please", "mutate")
			},
		},
		{
			name: "list, not in, mutate",
			spec: newObj([]interface{}{"one", "two"}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				in:      []interface{}{[]interface{}{"one", "two"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "replaced", "spec", "please", "mutate")
			},
		},
		{
			name: "list, not in, no mutate",
			spec: newObj([]interface{}{"one", "two"}, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				notIn:   []interface{}{[]interface{}{"one", "two"}},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, []interface{}{"one", "two"}, "spec", "please", "mutate")
			},
		},

		{
			name: "null, in, mutate",
			spec: newObj(nil, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				// use the JSON parser to make sure we see empty objects as JSON does.
				in: func() []interface{} {
					var out []interface{}
					if err := json.Unmarshal([]byte(`[null]`), &out); err != nil {
						panic(err)
					}
					return out
				}(),
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "replaced", "spec", "please", "mutate")
			},
		},
		{
			name: "null, in, no mutate",
			spec: newObj(nil, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				// use the JSON parser to make sure we see empty objects as JSON does.
				in: []interface{}{"2"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, nil, "spec", "please", "mutate")
			},
		},
		{
			name: "null, not in, no mutate",
			spec: newObj(nil, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				// use the JSON parser to make sure we see empty objects as JSON does.
				notIn: func() []interface{} {
					var out []interface{}
					if err := json.Unmarshal([]byte(`[null]`), &out); err != nil {
						panic(err)
					}
					return out
				}(),
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, nil, "spec", "please", "mutate")
			},
		},
		{
			name: "null, in, mutate",
			spec: newObj(nil, "please", "mutate"),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value:   makeValue("replaced"),
				path:    "spec.please.mutate",
				// use the JSON parser to make sure we see empty objects as JSON does.
				notIn: []interface{}{"2"},
			},
			fn: func(u *unstructured.Unstructured) error {
				return ensureObj(u, "replaced", "spec", "please", "mutate")
			},
		},

		// test nil
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator := newAssignMutator(test.cfg)
			obj := newFoo(test.spec)
			_, err := mutator.Mutate(obj)
			if err != nil {
				t.Fatalf("failed mutation: %s", err)
			}
			if err := test.fn(obj); err != nil {
				t.Errorf("failed test: %v", err)
			}
		})
	}
}

// TestApplyTo merely tests that ApplyTo is called, its internal
// logic is tested elsewhere.
func TestApplyTo(t *testing.T) {
	tests := []struct {
		name          string
		applyTo       []match.ApplyTo
		group         string
		version       string
		kind          string
		matchExpected bool
	}{
		{
			name: "matches applyTo",
			applyTo: []match.ApplyTo{{
				Groups:   []string{""},
				Kinds:    []string{"Foo"},
				Versions: []string{"v1"},
			}},
			group:         "",
			version:       "v1",
			kind:          "Foo",
			matchExpected: true,
		},
		{
			name: "does not match applyTo",
			applyTo: []match.ApplyTo{{
				Groups:   []string{""},
				Kinds:    []string{"Foo"},
				Versions: []string{"v1"},
			}},
			group:         "",
			version:       "v1",
			kind:          "Bar",
			matchExpected: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &assignTestCfg{applyTo: test.applyTo}
			cfg.path = "spec.hello"
			cfg.value = makeValue("bar")
			mutator := newAssignMutator(cfg)
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   test.group,
				Version: test.version,
				Kind:    test.kind,
			})
			matches := mutator.Matches(obj, nil)
			if matches != test.matchExpected {
				t.Errorf("Matches() = %t, expected %t", matches, test.matchExpected)
			}
		})
	}
}

var testPod = &v1.Pod{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "v1",
		Kind:       "Pod",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "opa",
		Namespace: "production",
		Labels:    map[string]string{"owner": "me.agilebank.demo"},
	},
	Spec: v1.PodSpec{
		Containers: []v1.Container{
			{
				Name:  "opa",
				Image: "openpolicyagent/opa:0.9.2",
				Args: []string{
					"run",
					"--server",
					"--addr=localhost:8080",
				},
				Ports: []v1.ContainerPort{
					{
						ContainerPort: 8080,
						Name:          "out-of-scope",
					},
					{
						ContainerPort: 8888,
						Name:          "unchanged",
					},
				},
			},
		},
	},
}

func TestAssign(t *testing.T) {
	tests := []struct {
		name string
		obj  *unstructured.Unstructured
		cfg  *assignTestCfg
		fn   func(*unstructured.Unstructured) error
	}{
		{
			name: "integer key value",
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
				path:    `spec.containers[name: opa].ports[containerPort: 8888].name`,
				value:   makeValue("modified"),
			},
			obj: newPod(testPod),
			fn: func(u *unstructured.Unstructured) error {
				var pod v1.Pod
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &pod)
				if err != nil {
					return err
				}

				if len(pod.Spec.Containers) != 1 {
					return fmt.Errorf("incorrect number of containers: %d", len(pod.Spec.Containers))
				}
				c := pod.Spec.Containers[0]
				if len(c.Ports) != 2 {
					return fmt.Errorf("incorrect number of ports: %d", len(c.Ports))
				}
				p := c.Ports[1]
				if p.ContainerPort != int32(8888) {
					return fmt.Errorf("incorrect containerPort: %d", p.ContainerPort)
				}
				if p.Name != "modified" {
					return fmt.Errorf("incorrect port name: %s", p.Name)
				}
				return nil
			},
		},
		{
			name: "new integer key value",
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
				path:    `spec.containers[name: opa].ports[containerPort: 2001].name`,
				value:   makeValue("added"),
			},
			obj: newPod(testPod),
			fn: func(u *unstructured.Unstructured) error {
				var pod v1.Pod
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &pod)
				if err != nil {
					return err
				}

				if len(pod.Spec.Containers) != 1 {
					return fmt.Errorf("incorrect number of containers: %d", len(pod.Spec.Containers))
				}
				c := pod.Spec.Containers[0]
				if len(c.Ports) != 3 {
					return fmt.Errorf("incorrect number of ports: %d", len(c.Ports))
				}
				p := c.Ports[2]
				if p.ContainerPort != int32(2001) {
					return fmt.Errorf("incorrect containerPort: %d", p.ContainerPort)
				}
				if p.Name != "added" {
					return fmt.Errorf("incorrect port name: %s", p.Name)
				}
				return nil
			},
		},
		{
			name: "truncated integer key value",
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
				path:    `spec.containers[name: opa].ports[containerPort: 4294967297].name`,
				value:   makeValue("added"),
			},
			obj: newPod(testPod),
			fn: func(u *unstructured.Unstructured) error {
				var pod v1.Pod
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &pod)
				if err != nil {
					return err
				}

				if len(pod.Spec.Containers) != 1 {
					return fmt.Errorf("incorrect number of containers: %d", len(pod.Spec.Containers))
				}
				c := pod.Spec.Containers[0]
				if len(c.Ports) != 3 {
					return fmt.Errorf("incorrect number of ports: %d", len(c.Ports))
				}
				p := c.Ports[2]
				// Note in this test case, the UnstructuredConverter truncates our 64bit containerPort down to 32bit.
				// The actual mutation was done in 64bit.
				if p.ContainerPort != int32(1) {
					return fmt.Errorf("incorrect containerPort: %d", p.ContainerPort)
				}
				if p.Name != "added" {
					return fmt.Errorf("incorrect port name: %s", p.Name)
				}
				return nil
			},
		},
		{
			name: "type mismatch for key value",
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
				path:    `spec.containers[name: opa].ports[containerPort: "8888"].name`,
				value:   makeValue("modified"),
			},
			obj: newPod(testPod),
			fn: func(u *unstructured.Unstructured) error {
				var pod v1.Pod
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &pod)
				if err == nil {
					return errors.New("expected type mismatch when deserializing mutated pod")
				}

				containers, err := nestedMapSlice(u.Object, "spec", "containers")
				if err != nil {
					return fmt.Errorf("fetching containers: %w", err)
				}
				if len(containers) != 1 {
					return fmt.Errorf("incorrect number of containers: %d", len(containers))
				}
				ports, err := nestedMapSlice(containers[0], "ports")
				if err != nil {
					return fmt.Errorf("fetching ports: %w", err)
				}
				if len(ports) != 3 {
					return fmt.Errorf("incorrect number of ports: %d", len(containers))
				}
				if ports[1]["containerPort"] != 8888 && ports[1]["name"] != "unchanged" {
					return fmt.Errorf("port was incorrectly modified: %v", ports[1])
				}
				if ports[2]["containerPort"] != "8888" && ports[1]["name"] != "modified" {
					return fmt.Errorf("type mismatched port was not added as expected: %v", ports[1])
				}

				return nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator := newAssignMutator(test.cfg)
			obj := test.obj.DeepCopy()
			_, err := mutator.Mutate(obj)
			if err != nil {
				t.Fatalf("failed mutation: %s", err)
			}
			if err := test.fn(obj); err != nil {
				t.Errorf("failed test: %v", err)
			}
		})
	}
}

func nestedMapSlice(u map[string]interface{}, fields ...string) ([]map[string]interface{}, error) {
	lst, ok, err := unstructured.NestedSlice(u, fields...)
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, len(lst))
	for i := range lst {
		v, ok := lst[i].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected type: %T, expected map[string]interface{}", lst[i])
		}
		out[i] = v
	}
	return out, nil
}
