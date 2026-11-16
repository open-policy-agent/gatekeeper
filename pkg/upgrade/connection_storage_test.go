package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	connectionv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/yaml"
)

func TestMain(m *testing.M) {
	logf.SetLogger(logr.Discard())
	os.Exit(m.Run())
}

const (
	connectionAlphaAPIVersion = "connection.gatekeeper.sh/v1alpha1"
	connectionBetaAPIVersion  = "connection.gatekeeper.sh/v1beta1"
	migrationNamespace        = "connection-migration"
	migrationConfigMarker     = "connection-config-must-not-appear-in-logs"
)

func TestConnectionStorageMigration(t *testing.T) {
	for _, tt := range []struct {
		name         string
		rejectLegacy bool
	}{
		{name: "name limit ratcheting"},
		{name: "validation rejects legacy rewrite", rejectLegacy: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testConnectionStorageMigration(t, tt.rejectLegacy)
		})
	}
}

func testConnectionStorageMigration(t *testing.T, rejectLegacy bool) {
	t.Helper()
	connectionResource := connectionv1alpha1.GroupVersion.WithResource("connections")
	finalCRD := migrationCRD(t, "connection-customresourcedefinition.yaml")
	require.Equal(t, 2, len(finalCRD.Spec.Versions), "regenerate the dual-version Connection CRD before running this test")
	var alpha *apiextensionsv1.CustomResourceDefinitionVersion
	for i := range finalCRD.Spec.Versions {
		version := &finalCRD.Spec.Versions[i]
		require.True(t, version.Served)
		nameLimit := version.Schema.OpenAPIV3Schema.Properties["metadata"].Properties["name"].MaxLength
		require.NotNil(t, nameLimit, "regenerate the Connection CRD after integrating the name limit")
		require.EqualValues(t, 63, *nameLimit)
		if rejectLegacy {
			// Transition rules are not ratcheted. This test-only rule models
			// validation that rejects a rewrite even when the name is unchanged.
			version.Schema.OpenAPIV3Schema.XValidations = append(version.Schema.OpenAPIV3Schema.XValidations, apiextensionsv1.ValidationRule{
				Rule:    "self.metadata.name == oldSelf.metadata.name && size(self.metadata.name) <= 63",
				Message: "test fixture rejects updates to legacy Connection names longer than 63 characters",
			})
		}
		switch version.Name {
		case connectionv1alpha1.GroupVersion.Version:
			require.False(t, version.Storage)
			alpha = version
		case connectionv1beta1.GroupVersion.Version:
			require.True(t, version.Storage)
		default:
			t.Fatalf("unexpected Connection version %q", version.Name)
		}
	}
	require.NotNil(t, alpha)
	oldCRD := finalCRD.DeepCopy()
	oldCRD.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{*alpha.DeepCopy()}
	oldCRD.Spec.Versions[0].Storage = true
	oldSchema := oldCRD.Spec.Versions[0].Schema.OpenAPIV3Schema
	// Reproduce Connections created before the metadata.name limit was introduced.
	metadataSchema := oldSchema.Properties["metadata"]
	if metadataSchema.Properties != nil {
		nameSchema := metadataSchema.Properties["name"]
		nameSchema.MaxLength = nil
		metadataSchema.Properties["name"] = nameSchema
		oldSchema.Properties["metadata"] = metadataSchema
	}
	oldSchema.XValidations = nil
	for _, rule := range alpha.Schema.OpenAPIV3Schema.XValidations {
		if !strings.Contains(rule.Rule, "metadata.name") {
			oldSchema.XValidations = append(oldSchema.XValidations, rule)
		}
	}
	environment := &envtest.Environment{
		UseExistingCluster: ptr.To(false),
		CRDs: []*apiextensionsv1.CustomResourceDefinition{
			oldCRD, migrationCRD(t, "constrainttemplate-customresourcedefinition.yaml"),
		},
	}
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	configuration, err := environment.Start()
	require.NoError(t, err)
	require.NotNil(t, environment.ControlPlane.Etcd)
	require.Contains(t, []string{"127.0.0.1", "::1", "localhost"}, environment.ControlPlane.Etcd.URL.Hostname(),
		"storage inspection must only reach this test's owned etcd")
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	require.NoError(t, connectionv1alpha1.AddToScheme(scheme))
	c, err := client.New(configuration, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: migrationNamespace}}))

	names := []string{"audit-export", "admission-export", strings.Repeat("a", 64)}
	before := make(map[string]*storedConnectionRecord)
	for _, name := range names {
		connection := migrationConnection(name)
		require.NoError(t, c.Create(ctx, connection))
		require.NoError(t, unstructured.SetNestedSlice(connection.Object, []interface{}{
			map[string]interface{}{
				"id": "migration-test", "connectionUID": string(connection.GetUID()),
				"observedGeneration": connection.GetGeneration(),
				"operations":         []interface{}{"audit"},
				"publishStatuses": []interface{}{
					map[string]interface{}{"source": "audit", "active": true},
				},
			},
		}, "status", "byPod"))
		require.NoError(t, c.Status().Update(ctx, connection))
		stored := storedConnection(t, environment, name)
		require.Equal(t, connectionAlphaAPIVersion, stored.GetAPIVersion(), "the fixture must actually be stored as alpha")
		require.Equal(t, connection.GetUID(), stored.GetUID())
		before[name] = stored
		t.Logf("BEFORE raw etcd: name=%s apiVersion=%s uid=%s etcdRevision=%s", name, stored.GetAPIVersion(), stored.GetUID(), stored.revision)
	}

	deployedCRD := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(finalCRD), deployedCRD))
	deployedCRD.Spec = *finalCRD.Spec.DeepCopy()
	require.NoError(t, c.Update(ctx, deployedCRD))
	dynamicClient, err := dynamic.NewForConfig(configuration)
	require.NoError(t, err)
	betaResource := dynamicClient.Resource(connectionv1beta1.GroupVersion.WithResource(connectionResource.Resource)).Namespace(migrationNamespace)
	require.Eventually(t, func() bool {
		_, getErr := betaResource.Get(ctx, names[0], metav1.GetOptions{})
		return getErr == nil
	}, 10*time.Second, 20*time.Millisecond)
	betaView, err := betaResource.Get(ctx, names[0], metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, connectionBetaAPIVersion, betaView.GetAPIVersion())
	require.Equal(t, connectionAlphaAPIVersion, storedConnection(t, environment, names[0]).GetAPIVersion(),
		"a beta GET is conversion only, not evidence of a storage rewrite")

	var discoveryFailures, listFailures, conflicts, rewritten, longAttempts, invalid atomic.Int32
	migrationConfig := rest.CopyConfig(configuration)
	migrationConfig.Wrap(func(base http.RoundTripper) http.RoundTripper {
		return migrationRoundTripper(func(request *http.Request) (*http.Response, error) {
			path := request.URL.Path
			alphaPath := "/apis/" + connectionAlphaAPIVersion
			switch {
			case request.Method == http.MethodGet && path == alphaPath && discoveryFailures.CompareAndSwap(0, 1):
				return migrationErrorResponse(request, apierrors.NewServiceUnavailable("injected discovery failure")), nil
			case request.Method == http.MethodGet && path == alphaPath+"/connections" && listFailures.CompareAndSwap(0, 1):
				return migrationErrorResponse(request, apierrors.NewServiceUnavailable("injected list failure")), nil
			case request.Method == http.MethodPut && strings.HasSuffix(path, "/connections/"+names[0]) && conflicts.CompareAndSwap(0, 1):
				return migrationErrorResponse(request, apierrors.NewConflict(connectionResource.GroupResource(), names[0], fmt.Errorf("injected conflict"))), nil
			}
			response, roundTripErr := base.RoundTrip(request)
			if roundTripErr == nil && request.Method == http.MethodPut && strings.HasPrefix(path, alphaPath+"/namespaces/"+migrationNamespace+"/connections/") {
				if strings.HasSuffix(path, "/"+names[2]) {
					longAttempts.Add(1)
					if response.StatusCode == http.StatusUnprocessableEntity {
						invalid.Add(1)
					}
				} else if response.StatusCode == http.StatusOK {
					rewritten.Add(1)
				}
			}
			return response, roundTripErr
		})
	})
	stop, healthy := startMigrationManager(t, migrationConfig, scheme)
	require.Eventually(t, func() bool { return rewritten.Load() == 2 && longAttempts.Load() > 0 }, time.Minute, 20*time.Millisecond)
	require.EqualValues(t, 1, discoveryFailures.Load())
	require.EqualValues(t, 1, listFailures.Load())
	require.EqualValues(t, 1, conflicts.Load())
	require.Eventually(t, healthy, 5*time.Second, 20*time.Millisecond)
	require.Never(t, func() bool { return !healthy() }, 200*time.Millisecond, 20*time.Millisecond)
	stop()

	for _, name := range names[:2] {
		after := storedConnection(t, environment, name)
		require.Equal(t, connectionBetaAPIVersion, after.GetAPIVersion())
		require.Equal(t, before[name].GetUID(), after.GetUID())
		require.Equal(t, before[name].Object["spec"], after.Object["spec"])
		require.Equal(t, before[name].Object["status"], after.Object["status"])
		require.NotEqual(t, before[name].revision, after.revision)
		t.Logf("AFTER raw etcd: name=%s apiVersion=%s uid=%s etcdRevision=%s; spec/status preserved", name, after.GetAPIVersion(), after.GetUID(), after.revision)
	}
	t.Logf("startup retries: discovery=%d list=%d conflict=%d; ordinaryRewrites=%d invalid=%d; unrelated manager runnable healthy",
		discoveryFailures.Load(), listFailures.Load(), conflicts.Load(), rewritten.Load(), invalid.Load())

	t.Run("legacy name", func(t *testing.T) {
		retained := storedConnection(t, environment, names[2])
		if rejectLegacy {
			require.EqualValues(t, 1, invalid.Load(), "a validation failure must not be silently treated as migration")
			require.Equal(t, before[names[2]].Object, retained.Object, "blocked legacy Connections must remain entirely unchanged")
			require.Equal(t, before[names[2]].revision, retained.revision)
			t.Logf("BLOCKED raw etcd: name=%s apiVersion=%s uid=%s; object unchanged after validation failure", retained.GetName(), retained.GetAPIVersion(), retained.GetUID())
		} else {
			require.Zero(t, invalid.Load(), "the shipped maxLength validation permits rewriting unchanged legacy names through ratcheting")
			require.Equal(t, connectionBetaAPIVersion, retained.GetAPIVersion())
			require.Equal(t, before[names[2]].GetUID(), retained.GetUID())
			require.Equal(t, before[names[2]].Object["spec"], retained.Object["spec"])
			require.Equal(t, before[names[2]].Object["status"], retained.Object["status"])
			require.NotEqual(t, before[names[2]].revision, retained.revision)
			t.Logf("RATCHETED raw etcd: name=%s apiVersion=%s uid=%s; legacy name/spec/status preserved", retained.GetName(), retained.GetAPIVersion(), retained.GetUID())
		}
		err := c.Create(ctx, migrationConnection(strings.Repeat("b", 64)))
		require.True(t, apierrors.IsInvalid(err), "new overlong names must still be rejected: %v", err)
		require.ErrorContains(t, err, "metadata.name")
	})

	t.Run("old clients and rollback", func(t *testing.T) {
		oldClientConnection := &connectionv1alpha1.Connection{}
		key := client.ObjectKey{Namespace: migrationNamespace, Name: names[0]}
		require.NoError(t, c.Get(ctx, key, oldClientConnection))
		require.Equal(t, before[key.Name].GetUID(), oldClientConnection.UID)
		oldClientConnection.Labels = map[string]string{"old-client": "still-supported"}
		require.NoError(t, c.Update(ctx, oldClientConnection))
		afterUpdate := storedConnection(t, environment, key.Name)
		require.Equal(t, connectionBetaAPIVersion, afterUpdate.GetAPIVersion())
		require.Equal(t, before[key.Name].GetUID(), afterUpdate.GetUID())
		require.Equal(t, before[key.Name].Object["spec"], afterUpdate.Object["spec"])
		require.Equal(t, before[key.Name].Object["status"], afterUpdate.Object["status"])
		created := &connectionv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{Name: "old-client-create", Namespace: key.Namespace},
			Spec:       oldClientConnection.Spec,
		}
		require.NoError(t, c.Create(ctx, created))
		require.Equal(t, connectionBetaAPIVersion, storedConnection(t, environment, created.Name).GetAPIVersion())
		list := &connectionv1alpha1.ConnectionList{}
		require.NoError(t, c.List(ctx, list, client.InNamespace(key.Namespace)))
		require.Len(t, list.Items, 4)
		require.NoError(t, c.Delete(ctx, created))
		require.True(t, apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(created), &connectionv1alpha1.Connection{})))

		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(finalCRD), deployedCRD))
		require.ElementsMatch(t, []string{connectionv1alpha1.GroupVersion.Version, connectionv1beta1.GroupVersion.Version}, deployedCRD.Status.StoredVersions,
			"the upgrader must not edit storedVersions to claim migration")
		rollback := deployedCRD.DeepCopy()
		rollback.Spec.Versions = oldCRD.Spec.Versions
		err := c.Update(ctx, rollback)
		require.True(t, apierrors.IsInvalid(err), "dropping beta while recorded as a storage version must be rejected: %v", err)
		require.ErrorContains(t, err, "storedVersions")
		require.ErrorContains(t, err, connectionv1beta1.GroupVersion.Version)
		require.NoError(t, c.Get(ctx, key, &connectionv1alpha1.Connection{}), "binary rollback can keep using alpha with the dual-version CRD retained")
		t.Log("alpha GET/LIST/CREATE/UPDATE/DELETE remain usable; alpha-only CRD rollback rejected while beta is recorded in storedVersions")
	})

	t.Run("optional discovery failure is nonfatal", func(t *testing.T) {
		retainedBefore := storedConnection(t, environment, names[2])
		var denied atomic.Int32
		deniedConfig := rest.CopyConfig(configuration)
		deniedConfig.Wrap(func(base http.RoundTripper) http.RoundTripper {
			return migrationRoundTripper(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/apis/"+connectionAlphaAPIVersion {
					denied.Add(1)
					return migrationErrorResponse(request, apierrors.NewForbidden(connectionResource.GroupResource(), "", fmt.Errorf("injected denial"))), nil
				}
				return base.RoundTrip(request)
			})
		})
		stop, healthy := startMigrationManager(t, deniedConfig, scheme)
		require.Eventually(t, func() bool { return denied.Load() > 0 && healthy() }, 10*time.Second, 20*time.Millisecond)
		require.Never(t, func() bool { return !healthy() }, 200*time.Millisecond, 20*time.Millisecond)
		stop()
		require.EqualValues(t, 1, denied.Load(), "a forbidden optional migration must be reported without futile retries")
		retainedAfter := storedConnection(t, environment, names[2])
		require.Equal(t, retainedBefore.Object, retainedAfter.Object)
		require.Equal(t, retainedBefore.revision, retainedAfter.revision)
	})
}

func migrationCRD(t *testing.T, name string) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	// Installable CRDs include validation patches absent from controller-gen bases.
	data, err := os.ReadFile(filepath.Join("..", "..", "manifest_staging", "charts", "gatekeeper", "crds", name))
	require.NoError(t, err)
	crd := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, yaml.Unmarshal(data, crd))
	return crd
}

func migrationConnection(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": connectionAlphaAPIVersion,
		"kind":       "Connection",
		"metadata": map[string]interface{}{
			"name": name, "namespace": migrationNamespace,
		},
		"spec": map[string]interface{}{
			"driver": "disk",
			"config": map[string]interface{}{
				"path": "/export", "redactionMarker": migrationConfigMarker,
				"nested": map[string]interface{}{"enabled": true, "values": []interface{}{"one", "two"}},
			},
		},
	}}
}

type storedConnectionRecord struct {
	unstructured.Unstructured
	revision string
}

func storedConnection(t *testing.T, environment *envtest.Environment, name string) *storedConnectionRecord {
	t.Helper()
	key := "/registry/connection.gatekeeper.sh/connections/" + migrationNamespace + "/" + name
	body, err := json.Marshal(struct {
		Key []byte `json:"key"`
	}{Key: []byte(key)})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, environment.ControlPlane.Etcd.URL.String()+"/v3/kv/range", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var result struct {
		Kvs []struct {
			Value       []byte `json:"value"`
			ModRevision string `json:"mod_revision"`
		} `json:"kvs"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
	require.Len(t, result.Kvs, 1, "expected exactly one raw etcd record at %s", key)
	stored := &storedConnectionRecord{revision: result.Kvs[0].ModRevision}
	require.NoError(t, stored.UnmarshalJSON(result.Kvs[0].Value))
	return stored
}

func startMigrationManager(t *testing.T, configuration *rest.Config, scheme *runtime.Scheme) (func(), func() bool) {
	t.Helper()
	mgr, err := manager.New(configuration, manager.Options{
		Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	require.NoError(t, AddToManager(mgr))
	var ticks atomic.Int32
	require.NoError(t, mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				ticks.Add(1)
			}
		}
	})))
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	var managerErr error
	go func() {
		managerErr = mgr.Start(ctx)
		close(finished)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case <-finished:
				require.NoError(t, managerErr, "optional export migration must not fail the manager")
			case <-time.After(10 * time.Second):
				t.Fatal("upgrade manager did not stop")
			}
		})
	}
	t.Cleanup(stop)
	healthy := func() bool {
		select {
		case <-finished:
			return false
		default:
			return ticks.Load() > 2
		}
	}
	return stop, healthy
}

type migrationRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip migrationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func migrationErrorResponse(request *http.Request, err *apierrors.StatusError) *http.Response {
	body, marshalErr := json.Marshal(err.Status())
	if marshalErr != nil {
		panic(marshalErr)
	}
	return &http.Response{
		StatusCode: int(err.ErrStatus.Code),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}
}
