package assign

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/testhelpers"
	path "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type assignTestCfg struct {
	value     mutationsunversioned.AssignField
	path      string
	pathTests []mutationsunversioned.PathTest
	applyTo   []match.ApplyTo
}

func makeExternalData(e *mutationsunversioned.ExternalData) mutationsunversioned.AssignField {
	return mutationsunversioned.AssignField{ExternalData: e}
}

func makeValue(v interface{}) mutationsunversioned.AssignField {
	return mutationsunversioned.AssignField{Value: &types.Anything{Value: v}}
}

func newAssignMutator(cfg *assignTestCfg) *Mutator {
	m := &mutationsunversioned.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name: "Foo",
		},
	}
	m.Spec.Parameters.Assign = cfg.value
	m.Spec.Location = cfg.path
	m.Spec.Parameters.PathTests = cfg.pathTests
	m.Spec.ApplyTo = cfg.applyTo
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

func newPod(pod *corev1.Pod) *unstructured.Unstructured {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		panic(fmt.Sprintf("converting pod to unstructured: %v", err))
	}
	return &unstructured.Unstructured{Object: u}
}

func ensureObj(u *unstructured.Unstructured, expected interface{}, path ...string) error {
	v, exists, err := unstructured.NestedFieldNoCopy(u.Object, path...)
	if err != nil {
		return fmt.Errorf("could not retrieve value: %w", err)
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
		return fmt.Errorf("could not retrieve value: %w", err)
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.please.greet.me", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:*].securityPolicy", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:sidecar]", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:sidecar].securityPolicy", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustExist}},
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
				pathTests: []mutationsunversioned.PathTest{{SubPath: "spec.containers[name:c2]", Condition: path.MustNotExist}},
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
				pathTests: []mutationsunversioned.PathTest{
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
				pathTests: []mutationsunversioned.PathTest{
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
				pathTests: []mutationsunversioned.PathTest{
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
				pathTests: []mutationsunversioned.PathTest{
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
			_, err := mutator.Mutate(&types.Mutable{Object: obj})
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
			matches, err := mutator.Matches(&types.Mutable{Object: obj, Source: types.SourceTypeDefault})
			require.NoError(t, err)
			if matches != test.matchExpected {
				t.Errorf("Matches() = %t, expected %t", matches, test.matchExpected)
			}
		})
	}
}

var testPod = &corev1.Pod{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "v1",
		Kind:       "Pod",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "opa",
		Namespace: "production",
		Labels:    map[string]string{"owner": "me.agilebank.demo"},
	},
	Spec: corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "opa",
				Image: "openpolicyagent/opa:0.9.2",
				Args: []string{
					"run",
					"--server",
					"--addr=localhost:8080",
				},
				Ports: []corev1.ContainerPort{
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
			name: "metadata value",
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				path:    `spec.value`,
				value:   mutationsunversioned.AssignField{FromMetadata: &mutationsunversioned.FromMetadata{Field: mutationsunversioned.ObjName}},
			},
			obj: newFoo(map[string]interface{}{}),
			fn: func(u *unstructured.Unstructured) error {
				v, exists, err := unstructured.NestedString(u.Object, "spec", "value")
				if err != nil {
					return err
				}
				if !exists {
					return errors.New("spec.value does not exist, wanted creation")
				}
				if v != "my-foo" {
					return fmt.Errorf("spec.value = %v; wanted %v", v, "my-foo")
				}
				return nil
			},
		},
		{
			name: "integer key value",
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
				path:    `spec.containers[name: opa].ports[containerPort: 8888].name`,
				value:   makeValue("modified"),
			},
			obj: newPod(testPod),
			fn: func(u *unstructured.Unstructured) error {
				var pod corev1.Pod
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
				var pod corev1.Pod
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
				var pod corev1.Pod
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
				var pod corev1.Pod
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
		{
			name: "external data placeholders",
			obj:  newPod(testPod),
			cfg: &assignTestCfg{
				applyTo: []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				value: makeExternalData(&mutationsunversioned.ExternalData{
					Provider:   "some-provider",
					DataSource: types.DataSourceValueAtLocation,
				}),
				path: "spec.containers[name:*].image",
				pathTests: []mutationsunversioned.PathTest{
					{SubPath: "spec.containers[name:*].image", Condition: path.MustExist},
				},
			},
			fn: func(u *unstructured.Unstructured) error {
				obj := []interface{}{
					map[string]interface{}{
						"name": "opa",
						"image": &mutationsunversioned.ExternalDataPlaceholder{
							Ref: &mutationsunversioned.ExternalData{
								Provider:   "some-provider",
								DataSource: types.DataSourceValueAtLocation,
							},
							ValueAtLocation: "openpolicyagent/opa:0.9.2",
						},
						"args": []interface{}{
							"run",
							"--server",
							"--addr=localhost:8080",
						},
						"ports": []interface{}{
							map[string]interface{}{
								"containerPort": int64(8080),
								"name":          "out-of-scope",
							},
							map[string]interface{}{
								"containerPort": int64(8888),
								"name":          "unchanged",
							},
						},
						"resources": map[string]interface{}{},
					},
				}
				return ensureObj(u, obj, "spec", "containers")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			*externaldata.ExternalDataEnabled = true
			defer func() {
				*externaldata.ExternalDataEnabled = false
			}()

			mutator := newAssignMutator(test.cfg)
			obj := test.obj.DeepCopy()
			_, err := mutator.Mutate(&types.Mutable{Object: obj})
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

func Test_Assign_errors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mut    *mutationsunversioned.Assign
		errMsg string
	}{
		{
			name:   "empty path",
			mut:    &mutationsunversioned.Assign{},
			errMsg: "empty path",
		},
		{
			name: "name > 63",
			mut: &mutationsunversioned.Assign{
				ObjectMeta: metav1.ObjectMeta{
					Name: testhelpers.BigName(),
				},
			},
			errMsg: core.ErrNameLength.Error(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutator, err := MutatorForAssign(tt.mut)

			require.ErrorContains(t, err, tt.errMsg)
			require.Nil(t, mutator)
		})
	}
}
