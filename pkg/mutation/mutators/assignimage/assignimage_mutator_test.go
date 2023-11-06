package assignimage

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/testhelpers"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type aiTestConfig struct {
	domain string
	path   string
	tag    string

	location  string
	pathTests []mutationsunversioned.PathTest
	applyTo   []match.ApplyTo
}

func newAIMutator(cfg *aiTestConfig) *Mutator {
	m := newAI(cfg)
	m2, err := MutatorForAssignImage(m)
	if err != nil {
		panic(err)
	}
	return m2
}

func newAI(cfg *aiTestConfig) *mutationsunversioned.AssignImage {
	m := &mutationsunversioned.AssignImage{
		ObjectMeta: metav1.ObjectMeta{
			Name: "Foo",
		},
	}
	m.Spec.Parameters.AssignDomain = cfg.domain
	m.Spec.Parameters.AssignPath = cfg.path
	m.Spec.Parameters.AssignTag = cfg.tag
	m.Spec.Location = cfg.location
	m.Spec.Parameters.PathTests = cfg.pathTests
	m.Spec.ApplyTo = cfg.applyTo
	return m
}

func newPod(imageVal, name string) *unstructured.Unstructured {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  name,
					Image: imageVal,
				},
			},
		},
	}

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		panic(fmt.Sprintf("converting pod to unstructured: %v", err))
	}
	return &unstructured.Unstructured{Object: u}
}

func newPodNoImage() *unstructured.Unstructured {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "foo",
				},
			},
		},
	}

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		panic(fmt.Sprintf("converting pod to unstructured: %v", err))
	}
	return &unstructured.Unstructured{Object: u}
}

func podTest(wantImage string) func(*unstructured.Unstructured) error {
	return func(u *unstructured.Unstructured) error {
		var pod corev1.Pod
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &pod)
		if err != nil {
			return err
		}

		if len(pod.Spec.Containers) != 1 {
			return fmt.Errorf("incorrect number of containers: %d", len(pod.Spec.Containers))
		}

		c := pod.Spec.Containers[0]
		if c.Image != wantImage {
			return fmt.Errorf("image incorrect, got: %q wanted %v", c.Image, wantImage)
		}

		return nil
	}
}

func TestMutate(t *testing.T) {
	tests := []struct {
		name string
		obj  *unstructured.Unstructured
		cfg  *aiTestConfig
		fn   func(*unstructured.Unstructured) error
	}{
		{
			name: "mutate tag",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				tag:      ":new",
			},
			obj: newPod("library/busybox:v1", "foo"),
			fn:  podTest("library/busybox:new"),
		},
		{
			name: "mutate path and tag with empty image",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				path:     "library/busybox",
				tag:      ":new",
			},
			obj: newPod("", "foo"),
			fn:  podTest("library/busybox:new"),
		},
		{
			name: "mutate path and tag with missing image",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				path:     "library/busybox",
				tag:      ":new",
			},
			obj: newPodNoImage(),
			fn:  podTest("library/busybox:new"),
		},
		{
			name: "mutate path",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				path:     "new/repo",
			},
			obj: newPod("library/busybox:v1", "foo"),
			fn:  podTest("new/repo:v1"),
		},
		{
			name: "mutate domain",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				domain:   "myreg.io",
			},
			obj: newPod("docker.io/library/busybox:v1", "foo"),
			fn:  podTest("myreg.io/library/busybox:v1"),
		},
		{
			name: "add domain",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				domain:   "myreg.io",
			},
			obj: newPod("library/busybox:v1", "foo"),
			fn:  podTest("myreg.io/library/busybox:v1"),
		},
		{
			name: "add tag",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				tag:      ":latest",
			},
			obj: newPod("myreg.io/library/busybox", "foo"),
			fn:  podTest("myreg.io/library/busybox:latest"),
		},
		{
			name: "add digest",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				tag:      "@sha256:12345678901234567890123456789012",
			},
			obj: newPod("myreg.io/library/busybox", "foo"),
			fn:  podTest("myreg.io/library/busybox@sha256:12345678901234567890123456789012"),
		},
		{
			name: "mutate all field",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				domain:   "myreg.io",
				path:     "newlib/newbox",
				tag:      ":v2",
			},
			obj: newPod("docker.io/library/busybox:v1", "foo"),
			fn:  podTest("myreg.io/newlib/newbox:v2"),
		},
		{
			name: "mutate path, domain not set",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				path:     "newlib/newbox",
			},
			obj: newPod("library/busybox:v1", "foo"),
			fn:  podTest("newlib/newbox:v1"),
		},
		{
			name: "mutate path and tag, no domain set",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				path:     "newlib/newbox",
				tag:      ":latest",
			},
			obj: newPod("library/busybox:v1", "foo"),
			fn:  podTest("newlib/newbox:latest"),
		},
		{
			name: "mutate tag to digest",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				tag:      "@sha256:12345678901234567890123456789012",
			},
			obj: newPod("library/busybox:v1", "foo"),
			fn:  podTest("library/busybox@sha256:12345678901234567890123456789012"),
		},
		{
			name: "mutate domain with bad imageref with no domain still converges",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				domain:   "myreg.io",
			},
			obj: newPod("this/not.good:ABC123_//lib.com.repo//localhost@blah101", "foo"),
			fn:  podTest("myreg.io/this/not.good:ABC123_//lib.com.repo//localhost@blah101"),
		},
		{
			name: "mutate domain with bad imageref with domain still converges",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				domain:   "myreg.io",
			},
			obj: newPod("a.b.c:5000//not.good:ABC123_//lib.com.repo//localhost@blah101", "foo"),
			fn:  podTest("myreg.io//not.good:ABC123_//lib.com.repo//localhost@blah101"),
		},
		{
			name: "mutate path and tag colon in imageref's domain still converges",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				path:     "repo/app",
				tag:      ":latest",
			},
			obj: newPod("a.b.c:/not.good:ABC123_//lib.com.repo//localhost@blah101", "foo"),
			fn:  podTest("a.b.c:/repo/app:latest"),
		},
		{
			name: "mutate path to domain-like string with domain set",
			cfg: &aiTestConfig{
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
				location: `spec.containers[name:foo].image`,
				domain:   "myreg.io",
				path:     "my.special.repo/a.b/c",
			},
			obj: newPod("a.b:latest", "foo"),
			fn:  podTest("myreg.io/my.special.repo/a.b/c:latest"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator := newAIMutator(test.cfg)
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

func TestMutatorForAssignImage(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *aiTestConfig
		errFn func(error) bool
	}{
		{
			name: "valid assignImage",
			cfg: &aiTestConfig{
				domain:   "a.b.c",
				path:     "new/app",
				tag:      ":latest",
				location: "spec.containers[name:foo].image",
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
			},
		},
		{
			name: "metadata root returns err",
			cfg: &aiTestConfig{
				domain:   "a.b.c",
				path:     "new/app",
				tag:      ":latest",
				location: "metadata.labels.image",
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
			},
			errFn: func(err error) bool {
				return errors.As(err, &metadataRootError{})
			},
		},
		{
			name: "terminal list returns err",
			cfg: &aiTestConfig{
				domain:   "a.b.c",
				path:     "new/app",
				tag:      ":latest",
				location: "spec.containers[name:foo]",
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
			},
			errFn: func(err error) bool {
				return errors.As(err, &listTerminalError{})
			},
		},
		{
			name: "syntactically invalid location returns err",
			cfg: &aiTestConfig{
				domain:   "a.b.c",
				path:     "new/app",
				tag:      ":latest",
				location: "/x/y/zx[)",
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
			},
			errFn: func(err error) bool {
				return strings.Contains(err.Error(), "invalid location format")
			},
		},
		{
			name: "bad assigns return err",
			cfg: &aiTestConfig{
				domain:   "",
				path:     "a.b.c/repo",
				tag:      ":latest",
				location: "spec.containers[name:foo].image",
				applyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Foo"}}},
			},
			errFn: func(err error) bool {
				return errors.As(err, &domainLikePathError{})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mut, err := MutatorForAssignImage(newAI(tc.cfg))
			if err != nil && mut != nil {
				t.Errorf("returned non-nil mutator but got err: %s", err)
			}
			if tc.errFn != nil {
				if err == nil {
					t.Errorf("wanted err but  got nil")
				} else if !tc.errFn(err) {
					t.Errorf("got error of unexpected type: %s", err)
				}
			}
		})
	}
}

func Test_AssignImage_errors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mut    *mutationsunversioned.AssignImage
		errMsg string
	}{
		{
			name:   "empty path",
			mut:    &mutationsunversioned.AssignImage{},
			errMsg: "empty path",
		},
		{
			name: "name > 63",
			mut: &mutationsunversioned.AssignImage{
				ObjectMeta: metav1.ObjectMeta{
					Name: testhelpers.BigName(),
				},
			},
			errMsg: core.ErrNameLength.Error(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutator, err := MutatorForAssignImage(tt.mut)

			require.ErrorContains(t, err, tt.errMsg)
			require.Nil(t, mutator)
		})
	}
}
