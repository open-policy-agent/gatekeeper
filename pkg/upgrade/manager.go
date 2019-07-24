package upgrade

// TODO consider whether this needs to exist after https://github.com/kubernetes/kubernetes/pull/79495
// is merged, or we make the minimum supported version of k8s v1.14

import (
	"context"
	"strings"
	"time"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("controller").WithValues("metaKind", "upgrade")

const (
	crdName       = "constrainttemplates.templates.gatekeeper.sh"
)

// UpgradeManager allows us to upgrade resources on startup
type UpgradeManager struct {
	client  client.Client
	cfg     *rest.Config
	ctx     context.Context
}

// New creates a new manager for audit
func New(ctx context.Context, cfg *rest.Config) (*UpgradeManager, error) {
	am := &UpgradeManager{
		cfg:     cfg,
		ctx:     ctx,
	}
	return am, nil
}


// Start implements the Runnable interface
func (um *UpgradeManager) Start(stop <-chan struct{}) error {
	log.Info("Starting Upgrade Manager")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer log.Info("Stopping upgrade manager workers")
	errCh := make(chan error)
	go func() {errCh <- um.upgrade(ctx)}()
	select {
	case <-stop:
		return nil
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	// We must block indefinitely or manager will exit
	<-stop
	return nil
}

func (um *UpgradeManager) ensureCRDExists(ctx context.Context) error {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{}
	return um.client.Get(ctx, types.NamespacedName{Name: crdName}, crd)
}

func (um *UpgradeManager) getAllKinds(groupVersion string) (*metav1.APIResourceList, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(um.cfg)
	if err != nil {
		return nil, err
	}
	return discoveryClient.ServerResourcesForGroupVersion(groupVersion)
}

func (um *UpgradeManager) upgrade(ctx context.Context) error {
	gvs := []string{
		"constraints.gatekeeper.sh/v1alpha1",
		"templates.gatekeeper.sh/v1alpha1",
	}
	for _, gv := range gvs {
		if err := um.upgradeGroupVersion(ctx, gv); err != nil {
			return err
		}
	}
	return nil
}

// upgradeGroupVersion touches each resource in a given groupVersion, incrementing its storage version
func (um *UpgradeManager) upgradeGroupVersion(ctx context.Context, groupVersion string) error {
	// new client to get updated restmapper
	c, err := client.New(um.cfg, client.Options{Scheme: nil, Mapper: nil})
	if err != nil {
		return err
	}
	um.client = c
	if err := um.ensureCRDExists(ctx); err != nil {
		log.Info("required crd has not been deployed ", "CRD", crdName)
		return err
	}
	// get all resource kinds
	resourceList, err := um.getAllKinds(groupVersion)
	if err != nil {
		// If the resource doesn't exist, it doesn't need upgrading
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	resourceGV := strings.Split(resourceList.GroupVersion, "/")
	group := resourceGV[0]
	version := resourceGV[1]

	// get resource for each Kind
	for _, r := range resourceList.APIResources {
		log.Info("resource", "kind", r.Kind)
		resourceGvk := schema.GroupVersionKind{
			Group:   group,
			Version: version,
			Kind:    r.Kind + "List",
		}
		instanceList := &unstructured.UnstructuredList{}
		instanceList.SetGroupVersionKind(resourceGvk)
		err := um.client.List(ctx, &client.ListOptions{}, instanceList)
		if err != nil {
			return err
		}
		log.Info("resoure", "count of resources", len(instanceList.Items))
		updateResources := make(map[string]unstructured.Unstructured, len(instanceList.Items))
		// get each resourcet
		for _, item := range instanceList.Items {
			updateResources[item.GetSelfLink()] = item
		}

		if len(updateResources) > 0 {
			urloop := &updateResourceLoop{
				ur:      updateResources,
				client:  um.client,
				stop:    make(chan struct{}),
				stopped: make(chan struct{}),
			}
			log.Info("starting update resources loop", "updateResources", updateResources)
			go urloop.update()
		}
	}
	return nil
}

type updateResourceLoop struct {
	ur      map[string]unstructured.Unstructured
	client  client.Client
	stop    chan struct{}
	stopped chan struct{}
}

func (urloop *updateResourceLoop) update() {
	defer close(urloop.stopped)
	updateLoop := func() (bool, error) {
		for _, item := range urloop.ur {
			select {
			case <-urloop.stop:
				return true, nil
			default:
				failure := false
				ctx := context.Background()
				var latestItem unstructured.Unstructured
				item.DeepCopyInto(&latestItem)
				name := latestItem.GetName()
				namespace := latestItem.GetNamespace()
				namespacedName := types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				}
				// get the latest constraint
				err := urloop.client.Get(ctx, namespacedName, &latestItem)
				if err != nil {
					failure = true
					log.Error(err, "could not get latest resource during update", "name", name, "namespace", namespace)
				}
				if err := urloop.client.Update(ctx, &latestItem); err != nil {
					failure = true
					log.Error(err, "could not update resource", "name", name, "namespace", namespace)
				}
				if !failure {
					delete(urloop.ur, latestItem.GetSelfLink())
				}
			}
		}
		if len(urloop.ur) == 0 {
			return true, nil
		}
		return false, nil
	}

	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Jitter:   1,
		Steps:    5,
	}, updateLoop); err != nil {
		log.Error(err, "could not update resource reached max retries", "remaining update resources", urloop.ur)
	}
}
