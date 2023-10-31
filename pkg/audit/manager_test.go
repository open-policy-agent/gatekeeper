package audit

import (
	"context"
	"os"
	"reflect"
	"testing"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_auditFromCache(t *testing.T) {
	podToReview := fakes.Pod(fakes.WithNamespace("test-namespace-1"))
	podGVK := podToReview.GroupVersionKind()
	testAuditCache := fakeCacheListerFor([]schema.GroupVersionKind{podGVK}, []client.Object{podToReview})

	driver, err := rego.New()
	require.NoError(t, err)
	client, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	require.NoError(t, err)

	_, err = client.AddTemplate(context.Background(), fakes.DenyAllRegoTemplate())
	require.NoError(t, err, "adding denyall constraint template")
	_, err = client.AddConstraint(context.Background(), fakes.DenyAllConstraint())
	require.NoError(t, err, "adding denyall constraint")

	tests := []struct {
		name            string
		processExcluder *process.Excluder
		wantViolation   bool
	}{
		{
			name:            "obj excluded from audit",
			processExcluder: processExcluderFor([]string{"test-namespace-1"}),
		},
		{
			name:            "obj not excluded from audit",
			processExcluder: processExcluderFor([]string{}),
			wantViolation:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			am := &Manager{
				processExcluder: tc.processExcluder,
				auditCache:      testAuditCache,
				opa:             client,
			}

			results, errs := am.auditFromCache(context.Background())
			require.Len(t, errs, 0)

			if tc.wantViolation {
				require.Len(t, results, 1)
			} else {
				require.Len(t, results, 0)
			}
		})
	}
}

func fakeCacheListerFor(gvks []schema.GroupVersionKind, objsToList []client.Object) *CacheLister {
	k8sclient := fake.NewClientBuilder().WithObjects(objsToList...).Build()
	fakeLister := fakeWatchIterator{gvksToList: gvks}

	return NewAuditCacheLister(k8sclient, &fakeLister)
}

type fakeWatchIterator struct {
	gvksToList []schema.GroupVersionKind
}

func (f *fakeWatchIterator) DoForEach(listFunc func(gvk schema.GroupVersionKind) error) error {
	for _, gvk := range f.gvksToList {
		if err := listFunc(gvk); err != nil {
			return err
		}
	}

	return nil
}

func processExcluderFor(ns []string) *process.Excluder {
	processExcluder := process.New()
	for _, n := range ns {
		processExcluder.Add([]configv1alpha1.MatchEntry{
			{
				ExcludedNamespaces: []wildcard.Wildcard{wildcard.Wildcard(n)},
				Processes:          []string{"audit"},
			},
		})
	}

	return processExcluder
}

func Test_newNSCache(t *testing.T) {
	tests := []struct {
		name string
		want *nsCache
	}{
		{
			name: "test",
			want: &nsCache{
				cache: map[string]corev1.Namespace{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newNSCache(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newNSCache() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_truncateString(t *testing.T) {
	type args struct {
		str  string
		size int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test 1",
			args: args{
				str:  "Hello world!",
				size: len("Hello world!"),
			},
			want: "Hello world!",
		},
		{
			name: "test 2",
			args: args{
				str:  "Hello world!",
				size: 5,
			},
			want: "He...",
		},
		{
			name: "test 3",
			args: args{
				str:  "Hello, world!",
				size: 0,
			},
			want: "...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateString(tt.args.str, tt.args.size); got != tt.want {
				t.Errorf("truncateString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_mergeErrors(t *testing.T) {
	t.Run("one error", func(t *testing.T) {
		errs := []error{errors.New("error 1")}
		expected := "error 1"
		result := mergeErrors(errs)
		if result == nil || result.Error() != expected {
			t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
		}
	})

	t.Run("empty errors", func(t *testing.T) {
		errs := []error{}
		expected := ""
		result := mergeErrors(errs)
		if result.Error() != expected {
			t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
		}
	})

	t.Run("3 errors", func(t *testing.T) {
		errs := []error{errors.New("error 1"), errors.New("error 2"), errors.New("error 3")}
		expected := "error 1\nerror 2\nerror 3"
		result := mergeErrors(errs)
		if result == nil || result.Error() != expected {
			t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
		}
	})

	t.Run("2 errors with newlines", func(t *testing.T) {
		errs := []error{errors.New("error 1\nerror 1.1"), errors.New("error 2\nerror 2.2")}
		expected := "error 1\nerror 1.1\nerror 2\nerror 2.2"
		result := mergeErrors(errs)
		if result == nil || result.Error() != expected {
			t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
		}
	})
}

func Test_nsMapFromObjs(t *testing.T) {
	tests := []struct {
		name       string
		objs       []unstructured.Unstructured
		want       map[string]*corev1.Namespace
		wantErr    bool
		errorMatch string
	}{
		{
			name: "two namespaces",
			objs: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Namespace",
						"metadata": map[string]interface{}{
							"name": "test-namespace-1",
						},
					},
				},
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Namespace",
						"metadata": map[string]interface{}{
							"name": "test-namespace-2",
						},
					},
				},
			},
			want: map[string]*corev1.Namespace{
				"test-namespace-1": {
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Namespace",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace-1",
					},
				},
				"test-namespace-2": {
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Namespace",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace-2",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := nsMapFromObjs(tt.objs)
			if (err != nil) != tt.wantErr {
				t.Errorf("nsMapFromObjs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("nsMapFromObjs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getViolationRef(t *testing.T) {
	type args struct {
		gkNamespace string
		rkind       string
		rname       string
		rnamespace  string
		rrv         string
		ckind       string
		cname       string
		cnamespace  string
		ruid        types.UID
		einvolved   bool
	}
	tests := []struct {
		name string
		args args
		want *corev1.ObjectReference
	}{
		{
			name: "Test case 1 - Gatekeeper Namespace",
			args: args{
				gkNamespace: "default",
				rkind:       "Pod",
				rname:       "my-pod",
				rnamespace:  "default",
				ckind:       "LimitRange",
				cname:       "my-limit-range",
				cnamespace:  "default",
				einvolved:   false,
			},
			want: &corev1.ObjectReference{
				Kind:      "Pod",
				Name:      "my-pod",
				UID:       "Pod/default/my-pod/LimitRange/default/my-limit-range",
				Namespace: "default",
			},
		},
		{
			name: "Test case 2 - GK Namespace",
			args: args{
				gkNamespace: "kube-system",
				rkind:       "Service",
				rname:       "my-service",
				rnamespace:  "default",
				ckind:       "PodSecurityPolicy",
				cname:       "my-pod-security-policy",
				cnamespace:  "kube-system",
				einvolved:   false,
			},
			want: &corev1.ObjectReference{
				Kind:      "Service",
				Name:      "my-service",
				UID:       "Service/default/my-service/PodSecurityPolicy/kube-system/my-pod-security-policy",
				Namespace: "kube-system",
			},
		},
		{
			name: "Test case 3 - Involved Namespace",
			args: args{
				gkNamespace: "kube-system",
				rkind:       "Pod",
				rname:       "my-pod",
				rrv:         "123456",
				ruid:        "abcde-123456",
				rnamespace:  "default",
				ckind:       "LimitRange",
				cname:       "my-limit-range",
				cnamespace:  "default",
				einvolved:   true,
			},
			want: &corev1.ObjectReference{
				Kind:            "Pod",
				Name:            "my-pod",
				Namespace:       "default",
				ResourceVersion: "123456",
				UID:             "abcde-123456",
			},
		},
		{
			name: "Test case 4 - Involved Namespace Cluster Scoped",
			args: args{
				gkNamespace: "kube-system",
				rkind:       "Service",
				rname:       "my-service",
				rrv:         "123456",
				ruid:        "abcde-123456",
				ckind:       "PodSecurityPolicy",
				cname:       "my-pod-security-policy",
				cnamespace:  "kube-system",
				einvolved:   true,
			},
			want: &corev1.ObjectReference{
				Kind:            "Service",
				Name:            "my-service",
				Namespace:       "kube-system",
				ResourceVersion: "123456",
				UID:             "abcde-123456",
			},
		},
		{
			name: "Test case 5 - Involved Namespace RV/UID",
			args: args{
				gkNamespace: "kube-system",
				rkind:       "Service",
				rname:       "my-service",
				rrv:         "",
				ruid:        "",
				rnamespace:  "default",
				ckind:       "PodSecurityPolicy",
				cname:       "my-pod-security-policy",
				cnamespace:  "kube-system",
				einvolved:   true,
			},
			want: &corev1.ObjectReference{
				Kind:            "Service",
				Name:            "my-service",
				Namespace:       "default",
				ResourceVersion: "",
				UID:             "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getViolationRef(tt.args.gkNamespace, tt.args.rkind, tt.args.rname, tt.args.rnamespace, tt.args.rrv, tt.args.ruid, tt.args.ckind, tt.args.cname, tt.args.cnamespace, tt.args.einvolved); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getViolationRef() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getFilesFromDir(t *testing.T) {
	am := Manager{}

	t.Run("Test case 1: directory does not exist", func(t *testing.T) {
		_, err := am.getFilesFromDir("/does/not/exist", 10)
		if err == nil {
			t.Errorf("Expected error when directory does not exist, got nil")
		}
	})

	t.Run("Test case 2: directory exists and is empty", func(t *testing.T) {
		emptyDir, err := os.MkdirTemp("", "empty-dir")
		if err != nil {
			t.Errorf("Failed to create temporary directory: %v", err)
		}
		defer os.RemoveAll(emptyDir)
		files, err := am.getFilesFromDir(emptyDir, 10)
		if err != nil {
			t.Errorf("Unexpected error when directory is empty: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("Expected 0 files when directory is empty, got %d", len(files))
		}
	})

	t.Run("Test case 3: directory exists and has some files", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "temp-dir")
		if err != nil {
			t.Errorf("Failed to create temporary directory: %v", err)
		}
		defer os.RemoveAll(tempDir)
		for i := 0; i < 15; i++ {
			file, err := os.CreateTemp(tempDir, "test-file-*.txt")
			if err != nil {
				t.Errorf("Failed to create temporary file: %v", err)
			}
			file.Close()
		}
		files, err := am.getFilesFromDir(tempDir, 10)
		if err != nil {
			t.Errorf("Unexpected error when directory has files: %v", err)
		}
		if len(files) != 15 {
			t.Errorf("Expected 15 files when directory has 15 files, got %d", len(files))
		}
	})
}

func Test_removeAllFromDir(t *testing.T) {
	am := Manager{}

	t.Run("Test case 1: directory does not exist", func(t *testing.T) {
		err := am.removeAllFromDir("/does/not/exist", 10)
		if err == nil {
			t.Errorf("Expected error when directory does not exist, got nil")
		}
	})

	t.Run("Test case 2: directory exists and is empty", func(t *testing.T) {
		emptyDir, err := os.MkdirTemp("", "empty-dir")
		if err != nil {
			t.Errorf("Failed to create temporary directory: %v", err)
		}
		defer os.RemoveAll(emptyDir)
		err = am.removeAllFromDir(emptyDir, 10)
		if err != nil {
			t.Errorf("Unexpected error when directory is empty: %v", err)
		}
	})

	t.Run("Test case 3: directory exists and has some files", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "temp-dir")
		if err != nil {
			t.Errorf("Failed to create temporary directory: %v", err)
		}
		defer os.RemoveAll(tempDir)
		for i := 0; i < 15; i++ {
			file, err := os.CreateTemp(tempDir, "test-file-*.txt")
			if err != nil {
				t.Errorf("Failed to create temporary file: %v", err)
			}
			file.Close()
		}
		err = am.removeAllFromDir(tempDir, 10)
		if err != nil {
			t.Errorf("Unexpected error when removing files from directory: %v", err)
		}
		files, err := am.getFilesFromDir(tempDir, 10)
		if err != nil {
			t.Errorf("Unexpected error when checking if directory is empty: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("Expected 0 files when all files have been removed, got %d", len(files))
		}
	})
}

func Test_readUnstructured(t *testing.T) {
	am := Manager{}

	t.Run("Test case 1: invalid JSON", func(t *testing.T) {
		_, err := am.readUnstructured([]byte("invalid json"))
		if err == nil {
			t.Errorf("Expected error when input is invalid JSON, got nil")
		}
	})

	t.Run("Test case 2: valid JSON", func(t *testing.T) {
		jsonBytes := []byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"my-namespace"}}
		`)
		u, err := am.readUnstructured(jsonBytes)
		if err != nil {
			t.Errorf("Unexpected error when input is valid JSON: %v", err)
		}
		if u.GetName() != "my-namespace" {
			t.Errorf("Expected name to be 'my-namespace', got %s", u.GetName())
		}
	})
}
