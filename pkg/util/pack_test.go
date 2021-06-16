package util

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestUnpackRequest(t *testing.T) {
	testCases := []struct {
		name        string
		request     reconcile.Request
		wantGVK     schema.GroupVersionKind
		wantRequest reconcile.Request
		wantErr     error
	}{
		{
			name:    "empty request",
			request: reconcile.Request{},
			wantErr: ErrInvalidPackedName,
		},
		{
			name: "invalid gvk",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "gvk:b:c",
				},
			},
			wantErr: ErrInvalidPackedName,
		},
		{
			name: "valid gvk",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "gvk:Role.v1.rbac:foo",
					Namespace: "shipping",
				},
			},
			wantGVK: schema.GroupVersionKind{Kind: "Role", Version: "v1", Group: "rbac"},
			wantRequest: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "foo",
					Namespace: "shipping",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gvk, request, err := UnpackRequest(tc.request)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got UnpackRequest() err = %v, want %v",
					err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.wantGVK, gvk); diff != "" {
				t.Errorf("got UnpackRequest() gvk diff: %v", diff)
			}
			if diff := cmp.Diff(tc.wantRequest, request); diff != "" {
				t.Errorf("got UnpackRequest() request diff: %v", diff)
			}
		})
	}
}

func TestEventPackerMapFunc(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
		want []reconcile.Request
	}{
		{
			name: "no object",
			obj:  nil,
			want: nil,
		},
		{
			name: "empty object",
			obj:  &unstructured.Unstructured{},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "gvk:.v1.:"}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := EventPackerMapFunc()(tc.obj)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("got EventPackerMapFunc()(obj) diff: %v", diff)
			}

			for _, r := range got {
				_, _, err := UnpackRequest(r)
				if err != nil {
					t.Errorf("got invalid Request: %v", err)
				}
			}
		})
	}
}
