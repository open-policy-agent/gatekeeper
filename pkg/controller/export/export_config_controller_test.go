package export

import (
	"context"
	"flag"
	"fmt"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/dapr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcile(t *testing.T) {
	// Create a fake client with some data
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		t.Fatalf("Unexpected error parsing flag: %v", err)
	}

	err = flag.CommandLine.Parse([]string{"--enable-violation-export", "true"})
	if err != nil {
		t.Fatalf("Unexpected error parsing flag: %v", err)
	}

	request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: util.GetNamespace(), Name: dapr.Name}}

	ctx := context.Background()
	testCases := []struct {
		name     string
		config   *corev1.ConfigMap
		wantErr  bool
		errorMsg string
	}{
		{
			name: "invalid configmap",
			config: &corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Name:      dapr.Name,
					Namespace: util.GetNamespace(),
				},
			},
			wantErr:  true,
			errorMsg: fmt.Sprintf("data missing in configmap %s, unable to configure exporter", request.NamespacedName),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.config).Build()
			r := &Reconciler{
				Client: client,
				scheme: scheme,
			}

			_, err := r.Reconcile(ctx, request)
			if tc.wantErr {
				assert.Equal(t, err.Error(), tc.errorMsg)
			}
		})
	}
}
