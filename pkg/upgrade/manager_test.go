package upgrade

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-logr/zapr"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestConnectionUpdateResourceLoop(t *testing.T) {
	resource := schema.GroupResource{Group: connectionv1alpha1.GroupVersion.Group, Resource: "connections"}
	longName := strings.Repeat("a", 64)
	for _, tt := range []struct {
		name        string
		objectName  string
		getError    error
		updateError error
		persistent  bool
		canceled    bool
		cancelOnGet bool
		wantGets    int
		wantUpdates int
		wantPending int
		wantBlocked bool
	}{
		{name: "rewrite", wantGets: 1, wantUpdates: 1},
		{name: "transient read", getError: apierrors.NewServiceUnavailable("read unavailable"), wantGets: 2, wantUpdates: 1},
		{name: "conflict rereads latest object", updateError: apierrors.NewConflict(resource, "connection", errors.New("conflict")), wantGets: 2, wantUpdates: 2},
		{name: "deleted between list and get", getError: apierrors.NewNotFound(resource, "connection"), wantGets: 1},
		{name: "forbidden read", getError: fmt.Errorf("wrapped: %w", apierrors.NewForbidden(resource, "connection", errors.New("denied"))), wantGets: 1, wantPending: 1, wantBlocked: true},
		{
			name: "legacy long name is retained", objectName: longName,
			updateError: apierrors.NewInvalid(schema.GroupKind{Group: resource.Group, Kind: "Connection"}, longName,
				field.ErrorList{field.TooLong(field.NewPath("metadata", "name"), longName, 63)}),
			wantGets: 1, wantUpdates: 1, wantPending: 1, wantBlocked: true,
		},
		{name: "exhausted read retries", getError: apierrors.NewServiceUnavailable("read unavailable"), persistent: true, wantGets: 5, wantPending: 1},
		{name: "exhausted conflicts", updateError: apierrors.NewConflict(resource, "connection", errors.New("conflict")), persistent: true, wantGets: 5, wantUpdates: 5, wantPending: 1},
		{name: "canceled", canceled: true, wantPending: 1},
		{name: "canceled during retry", cancelOnGet: true, getError: apierrors.NewServiceUnavailable("read unavailable"), wantGets: 1, wantPending: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Exercise the production backoff without sleeping in the unit tests.
			synctest.Test(t, func(t *testing.T) {
				name := tt.objectName
				if name == "" {
					name = "connection"
				}
				initial := migrationConnection(name)
				initial.SetUID("preserved-uid")
				stale := initial.DeepCopy()
				require.NoError(t, unstructured.SetNestedField(stale.Object, "stale", "spec", "driver"))
				scheme := runtime.NewScheme()
				require.NoError(t, connectionv1alpha1.AddToScheme(scheme))
				baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initial).Build()
				expected := initial.DeepCopy()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				gets, updates := 0, 0
				c := interceptor.NewClient(baseClient, interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
						gets++
						if tt.cancelOnGet {
							cancel()
						}
						if tt.getError != nil && (tt.persistent || gets == 1) {
							if apierrors.IsNotFound(tt.getError) {
								require.NoError(t, c.Delete(context.Background(), initial))
							}
							return tt.getError
						}
						return c.Get(ctx, key, object, opts...)
					},
					Update: func(ctx context.Context, c client.WithWatch, object client.Object, opts ...client.UpdateOption) error {
						updates++
						if tt.updateError != nil && (tt.persistent || updates == 1) {
							if apierrors.IsConflict(tt.updateError) && !tt.persistent {
								require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(initial), expected))
								require.NoError(t, unstructured.SetNestedField(expected.Object, "concurrent-writer", "spec", "config", "revision"))
								require.NoError(t, c.Update(ctx, expected))
							}
							return tt.updateError
						}
						return c.Update(ctx, object, opts...)
					},
				})
				core, logs := observer.New(zap.InfoLevel)
				loop := &updateResourceLoop{
					ur:           map[util.KindVersionName]unstructured.Unstructured{util.GetUniqueKey(*stale): *stale},
					client:       c,
					stop:         make(chan struct{}),
					stopped:      make(chan struct{}),
					groupVersion: connectionGroupVersion,
					logger:       zapr.NewLogger(zap.New(core)),
				}
				if tt.canceled {
					cancel()
				}
				started := time.Now()
				loop.update(ctx)
				if tt.canceled || tt.cancelOnGet {
					require.Less(t, time.Since(started), time.Second, "cancellation must interrupt backoff")
				}
				require.Equal(t, tt.wantGets, gets)
				require.Equal(t, tt.wantUpdates, updates)
				require.Len(t, loop.ur, tt.wantPending)
				select {
				case <-loop.stopped:
				default:
					t.Fatal("update loop did not finish")
				}
				saved := initial.DeepCopy()
				err := baseClient.Get(context.Background(), client.ObjectKeyFromObject(initial), saved)
				if apierrors.IsNotFound(tt.getError) {
					require.True(t, apierrors.IsNotFound(err), "migration must not recreate a deleted Connection")
				} else {
					require.NoError(t, err)
					require.Equal(t, expected.GetUID(), saved.GetUID())
					require.Equal(t, expected.Object["spec"], saved.Object["spec"], "the latest spec must survive retries and validation errors")
				}
				if tt.wantPending != 0 {
					require.Zero(t, logs.FilterMessage("Connection storage migration completed").Len())
					entries := logs.FilterMessageSnippet("Connection storage migration incomplete").All()
					require.Len(t, entries, 1)
					require.Contains(t, entries[0].Message, "Retain", "operator guidance must explain how to preserve rollback")
					require.Contains(t, entries[0].Message, "restart Gatekeeper to retry")
					require.Equal(t, name, entries[0].ContextMap()["name"])
					require.Equal(t, initial.GetNamespace(), entries[0].ContextMap()["namespace"])
					require.Equal(t, tt.wantBlocked, entries[0].ContextMap()["blocked"])
				} else {
					require.Equal(t, 1, logs.FilterMessage("Connection storage migration completed").Len())
				}
				require.NotContains(t, fmt.Sprint(logs.All()), migrationConfigMarker)
			})
		})
	}
}
