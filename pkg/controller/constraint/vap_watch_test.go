package constraint

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type vapWatchCache struct {
	cache.Cache
	policyFailures int
}

func (testCache *vapWatchCache) GetInformer(_ context.Context, object client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	if _, isPolicy := object.(*admissionregistrationv1.ValidatingAdmissionPolicy); isPolicy && testCache.policyFailures > 0 {
		testCache.policyFailures--
		return nil, errors.New("policy informer temporarily unavailable")
	}
	return nil, nil
}

type vapWatchController struct {
	controller.Controller
	registrations int
}

func (testController *vapWatchController) Watch(source.Source) error {
	testController.registrations++
	return nil
}

func TestConstraintVAPWatchInitialization(t *testing.T) {
	configureVAP(t, vapTestConfig{apiEnabled: ptr.To(true)})
	transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
	testController := &vapWatchController{}
	watches := &constraintVAPWatches{
		controller: testController, cache: &vapWatchCache{policyFailures: 1}, generationMode: VAPGenerationModeConstraint,
	}
	complete, err := watches.start(context.Background())
	require.Error(t, err)
	require.False(t, complete)
	require.True(t, watches.bindingStarted)
	require.False(t, watches.policyStarted)
	require.Equal(t, 1, testController.registrations)
	complete, err = watches.start(context.Background())
	require.NoError(t, err)
	require.True(t, complete)
	require.Equal(t, 2, testController.registrations, "retry must not duplicate the already registered binding source")
	complete, err = watches.start(context.Background())
	require.NoError(t, err)
	require.True(t, complete)
	require.Equal(t, 2, testController.registrations)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, (&constraintVAPWatches{}).Start(ctx), "cancellation before startup must not access dependencies")
}

func TestConstraintVAPWatchesRecoverAfterDiscovery(t *testing.T) {
	environment := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	environment.ControlPlane.GetAPIServer().Configure().Append("runtime-config", "api/all=true")
	configuration, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	require.NoError(t, apis.AddToScheme(clientgoscheme.Scheme))
	require.NoError(t, admissionregistrationv1beta1.AddToScheme(clientgoscheme.Scheme))
	for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
		for _, mode := range []VAPGenerationMode{VAPGenerationModeTemplate, VAPGenerationModeConstraint} {
			t.Run(version+"/"+string(mode), func(t *testing.T) {
				configureVAP(t, vapTestConfig{generationMode: ptr.To(mode)})
				transform.SetVapAPIEnabled(nil)
				transform.SetGroupVersion(nil)
				backend, err := url.Parse(configuration.Host)
				require.NoError(t, err)
				proxy := httputil.NewSingleHostReverseProxy(backend)
				proxy.Transport, err = rest.TransportFor(configuration)
				require.NoError(t, err)
				var available atomic.Bool
				var failures atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if request.URL.Path == "/apis/admissionregistration.k8s.io/v1" || request.URL.Path == "/apis/admissionregistration.k8s.io/v1beta1" {
						if !available.Load() {
							failures.Add(1)
							http.Error(writer, "discovery temporarily unavailable", http.StatusServiceUnavailable)
							return
						}
						if version == vapAPIVersionV1Beta1 && request.URL.Path == "/apis/admissionregistration.k8s.io/v1" {
							http.NotFound(writer, request)
							return
						}
					}
					proxy.ServeHTTP(writer, request)
				}))
				t.Cleanup(server.Close)
				kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
				require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
					Clusters:       map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}},
					Contexts:       map[string]*clientcmdapi.Context{"test": {Cluster: "test"}},
					CurrentContext: "test",
				}, kubeconfig))
				testutils.Setenv(t, "KUBECONFIG", kubeconfig)
				manager, _ := testutils.SetupManager(t, configuration)
				var reconciles atomic.Int64
				require.NoError(t, add(manager, reconcile.Func(func(context.Context, reconcile.Request) (reconcile.Result, error) {
					reconciles.Add(1)
					return reconcile.Result{}, nil
				}), make(chan event.GenericEvent)))
				ctx := context.Background()
				testutils.StartManager(ctx, t, manager)
				require.Eventually(t, func() bool { return failures.Load() >= 2 }, 10*time.Second, 20*time.Millisecond)
				available.Store(true)
				require.Eventually(t, func() bool {
					enabled, discovered := transform.IsVapAPIEnabled(&log)
					return enabled && discovered != nil && discovered.Version == version
				}, 10*time.Second, 20*time.Millisecond)
				groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
				binding, err := vapBindingForVersion(groupVersion)
				require.NoError(t, err)
				bindingName := fmt.Sprintf("watch-binding-%s-%s", version, mode)
				binding.SetName(bindingName)
				switch typed := binding.(type) {
				case *admissionregistrationv1.ValidatingAdmissionPolicyBinding:
					typed.Spec = admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: "watch-policy", ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Audit}}
				case *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding:
					typed.Spec = admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{PolicyName: "watch-policy", ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Audit}}
				}
				objects := []client.Object{binding}
				if mode == VAPGenerationModeConstraint {
					definition := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{
						ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("watch-policy-%s-%s", version, mode)},
						Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicySpec{
							MatchConstraints: &admissionregistrationv1beta1.MatchResources{ResourceRules: []admissionregistrationv1beta1.NamedRuleWithOperations{{RuleWithOperations: admissionregistrationv1beta1.RuleWithOperations{
								Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create},
								Rule:       admissionregistrationv1beta1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}},
							}}}},
							Validations: []admissionregistrationv1beta1.Validation{{Expression: "true"}},
						},
					}
					policy, err := getRunTimeVAP(&groupVersion, definition, nil)
					require.NoError(t, err)
					objects = append(objects, policy)
				}
				for _, object := range objects {
					object.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "constraints.gatekeeper.sh/v1beta1", Kind: "WatchConstraint", Name: "watch-owner", UID: types.UID("watch-owner"), Controller: ptr.To(true)}})
					before := reconciles.Load()
					require.NoError(t, manager.GetClient().Create(ctx, object))
					require.Eventually(t, func() bool { return reconciles.Load() > before }, 10*time.Second, 20*time.Millisecond, "create event for %T must enqueue its Constraint after discovery recovers", object)
					require.NoError(t, manager.GetClient().Get(ctx, client.ObjectKeyFromObject(object), object))
					before = reconciles.Load()
					object.SetAnnotations(map[string]string{"test": "changed"})
					require.NoError(t, manager.GetClient().Update(ctx, object))
					require.Eventually(t, func() bool { return reconciles.Load() > before }, 10*time.Second, 20*time.Millisecond, "update event for %T must enqueue its Constraint", object)
					before = reconciles.Load()
					require.NoError(t, manager.GetClient().Delete(ctx, object))
					require.Eventually(t, func() bool { return reconciles.Load() > before }, 10*time.Second, 20*time.Millisecond, "delete event for %T must enqueue its Constraint", object)
				}
			})
		}
	}
}
