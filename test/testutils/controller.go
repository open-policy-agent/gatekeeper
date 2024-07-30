package testutils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	vendorCRDPath = []string{"vendor", "github.com", "open-policy-agent", "frameworks", "constraint", "deploy", "crds.yaml"}
	gkCRDPath     = []string{"config", "crd", "bases"}
)

// ConstantRetry makes 3,000 attempts at a rate of 100 per second. Since this
// is a test instance and not a "real" cluster, this is fine and there's no need
// to increase the wait time each iteration.
var ConstantRetry = wait.Backoff{
	Steps:    3000,
	Duration: 10 * time.Millisecond,
}

// CreateGatekeeperNamespace bootstraps the gatekeeper-system namespace for use in tests.
func CreateGatekeeperNamespace(cfg *rest.Config) error {
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		return err
	}

	// Create gatekeeper namespace
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-system",
		},
	}

	ctx := context.Background()
	_, err = controllerutil.CreateOrUpdate(ctx, c, ns, func() error { return nil })
	return err
}

// DeleteObjectAndConfirm returns a callback which deletes obj from the passed
// Client. Does result in mutations to obj. The callback includes a cached copy
// of all information required to delete obj in the callback, so it is safe to
// mutate obj afterwards. Similarly - client.Delete mutates its input, but
// the callback does not call client.Delete on obj. Instead, it creates a
// single-purpose Unstructured for this purpose. Thus, obj is not mutated after
// the callback is run.
func DeleteObjectAndConfirm(ctx context.Context, t *testing.T, c client.Client, obj client.Object) func() {
	t.Helper()

	// Cache the identifying information from obj. We refer to this cached
	// information in the callback, and not obj itself.
	gvk := obj.GetObjectKind().GroupVersionKind()
	namespace := obj.GetNamespace()
	name := obj.GetName()

	if gvk.Empty() {
		// We can't send a proper delete request with an Unstructured without
		// filling in GVK. The alternative would be to require tests to construct
		// a valid Scheme or provide a factory method for the type to delete - this
		// is easier.
		t.Fatalf("gvk for %v/%v %T is empty",
			namespace, name, obj)
	}

	return func() {
		t.Helper()

		// Construct a single-use Unstructured to send the Delete request.
		toDelete := fakes.UnstructuredFor(gvk, namespace, name)
		err := c.Delete(ctx, toDelete)
		if apierrors.IsNotFound(err) {
			return
		} else if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// Construct a single-use Unstructured to send the Get request. It isn't
			// safe to reuse Unstructureds for each retry as Get modifies its input.
			toGet := fakes.UnstructuredFor(gvk, namespace, name)
			key := client.ObjectKey{Namespace: namespace, Name: name}
			err2 := c.Get(ctx, key, toGet)
			if apierrors.IsGone(err2) || apierrors.IsNotFound(err2) {
				return nil
			}

			// Marshal the currently-gotten object, so it can be printed in test
			// failure output.
			s, _ := json.MarshalIndent(toGet, "", "  ")
			return fmt.Errorf("found %v %v:\n%s", gvk, key, string(s))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func StartControlPlane(m *testing.M, cfg **rest.Config, testerDepth int) {
	walkbacks := make([]string, testerDepth)
	for i := 0; i < testerDepth; i++ {
		walkbacks[i] = ".."
	}
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(append(walkbacks, vendorCRDPath...)...),
			filepath.Join(append(walkbacks, gkCRDPath...)...),
		},
		ErrorIfCRDPathMissing: true,
	}
	///TODO(ritazh): remove when vap is GAed in k/k
	args := t.ControlPlane.GetAPIServer().Configure()
	args.Append("runtime-config", "api/all=true")
	args.Append("feature-gates", "ValidatingAdmissionPolicy=true")

	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		log.Fatal(err)
	}

	if err := admissionregistrationv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatal(err)
	}

	var err error
	if *cfg, err = t.Start(); err != nil {
		log.Fatal(err)
	}
	log.Print("STARTED")

	code := m.Run()
	if err = t.Stop(); err != nil {
		log.Printf("error while trying to stop server: %v", err)
	}
	os.Exit(code)
}

// CreateThenCleanup creates obj in Client, and then registers obj to be deleted
// at the end of the test. The passed obj is safely deepcopied before being
// passed to client.Create, so it is not mutated by this call.
func CreateThenCleanup(ctx context.Context, t *testing.T, c client.Client, obj client.Object) {
	t.Helper()
	cpy := obj.DeepCopyObject()
	cpyObj, ok := cpy.(client.Object)
	if !ok {
		t.Fatalf("got obj.DeepCopyObject() type = %T, want %T", cpy, client.Object(nil))
	}
	err := c.Create(ctx, cpyObj)
	if err != nil {
		t.Fatal(err)
	}

	// It is unnecessary to deepcopy obj as deleteObjectAndConfirm does not pass
	// obj to any Client calls.
	t.Cleanup(DeleteObjectAndConfirm(ctx, t, c, obj))
}

func SetupDataClient(t *testing.T) *constraintclient.Client {
	driver, err := rego.New(rego.Tracing(false))
	if err != nil {
		t.Fatalf("setting up Driver: %v", err)
	}

	client, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
	if err != nil {
		t.Fatalf("setting up constraint framework client: %v", err)
	}
	return client
}

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, *sync.Map) {
	var requests sync.Map
	fn := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(ctx, req)
		requests.Store(req, struct{}{})
		return result, err
	})
	return fn, &requests
}
