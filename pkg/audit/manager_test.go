package audit

import (
	"reflect"
	"testing"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
	// Test case 1
	str := "Hello world!"
	size := len(str)
	expected := str
	result := truncateString(str, size)
	if result != expected {
		t.Errorf("Unexpected result for str = %q, size = %d: got %q, want %q", str, size, result, expected)
	}

	// Test case 2
	str = "Hello world!"
	size = 5
	expected = "He..."
	result = truncateString(str, size)
	if result != expected {
		t.Errorf("Unexpected result for str = %q, size = %d: got %q, want %q", str, size, result, expected)
	}

	// Test case 3
	str = "Hello, world!"
	size = 0
	expected = "..."
	result = truncateString(str, size)
	if result != expected {
		t.Errorf("Unexpected result for str = %q, size = %d: got %q, want %q", str, size, result, expected)
	}
}

func Test_mergeErrors(t *testing.T) {
	// one error
	errs := []error{errors.New("error 1")}
	expected := "error 1"
	result := mergeErrors(errs)
	if result == nil || result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}

	// empty errors
	errs = []error{}
	expected = ""
	result = mergeErrors(errs)
	if result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}

	// 3 errors
	errs = []error{errors.New("error 1"), errors.New("error 2"), errors.New("error 3")}
	expected = "error 1\nerror 2\nerror 3"
	result = mergeErrors(errs)
	if result == nil || result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}

	// 2 errors with newlines
	errs = []error{errors.New("error 1\nerror 1.1"), errors.New("error 2\nerror 2.2")}
	expected = "error 1\nerror 1.1\nerror 2\nerror 2.2"
	result = mergeErrors(errs)
	if result == nil || result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}
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
		ckind       string
		cname       string
		cnamespace  string
	}
	tests := []struct {
		name string
		args args
		want *corev1.ObjectReference
	}{
		{
			name: "Test case 1",
			args: args{
				gkNamespace: "default",
				rkind:       "Pod",
				rname:       "my-pod",
				rnamespace:  "default",
				ckind:       "LimitRange",
				cname:       "my-limit-range",
				cnamespace:  "default",
			},
			want: &corev1.ObjectReference{
				Kind:      "Pod",
				Name:      "my-pod",
				UID:       "Pod/default/my-pod/LimitRange/default/my-limit-range",
				Namespace: "default",
			},
		},
		{
			name: "Test case 2",
			args: args{
				gkNamespace: "kube-system",
				rkind:       "Service",
				rname:       "my-service",
				rnamespace:  "default",
				ckind:       "PodSecurityPolicy",
				cname:       "my-pod-security-policy",
				cnamespace:  "kube-system",
			},
			want: &corev1.ObjectReference{
				Kind:      "Service",
				Name:      "my-service",
				UID:       "Service/default/my-service/PodSecurityPolicy/kube-system/my-pod-security-policy",
				Namespace: "kube-system",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getViolationRef(tt.args.gkNamespace, tt.args.rkind, tt.args.rname, tt.args.rnamespace, tt.args.ckind, tt.args.cname, tt.args.cnamespace); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getViolationRef() = %v, want %v", got, tt.want)
			}
		})
	}
}
