/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"flag"
	"os"
	"sync"

	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var debugUseFakePod = flag.Bool("debug-use-fake-pod", false, "Use a fake pod name so the Gatekeeper executable can be run outside of Kubernetes")

type Injector interface {
	InjectOpa(*constraintclient.Client)
	InjectWatchManager(*watch.Manager)
	InjectControllerSwitch(*watch.ControllerSwitch)
	InjectTracker(tracker *readiness.Tracker)
	InjectMutationSystem(mutationSystem *mutation.System)
	InjectExpansionSystem(expansionSystem *expansion.System)
	InjectProviderCache(providerCache *externaldata.ProviderCache)
	Add(mgr manager.Manager) error
}

type GetPodInjector interface {
	InjectGetPod(func(context.Context) (*corev1.Pod, error))
}

type GetProcessExcluderInjector interface {
	InjectProcessExcluder(processExcluder *process.Excluder)
}

type WatchSetInjector interface {
	InjectWatchSet(watchSet *watch.Set)
}

type PubsubInjector interface {
	InjectPubsubSystem(pubsubSystem *pubsub.System)
}

type CacheManagerInjector interface {
	InjectCacheManager(cm *cm.CacheManager)
}

// Injectors is a list of adder structs that need injection. We can convert this
// to an interface once we create controllers for things like data sync.
var Injectors []Injector

// AddToManagerFuncs is a list of functions to add all Controllers to the Manager.
var AddToManagerFuncs []func(manager.Manager) error

// Dependencies are dependencies that can be injected into controllers.
type Dependencies struct {
	Opa              *constraintclient.Client
	WatchManger      *watch.Manager
	ControllerSwitch *watch.ControllerSwitch
	Tracker          *readiness.Tracker
	GetPod           func(context.Context) (*corev1.Pod, error)
	ProcessExcluder  *process.Excluder
	MutationSystem   *mutation.System
	ExpansionSystem  *expansion.System
	ProviderCache    *externaldata.ProviderCache
	WatchSet         *watch.Set
	PubsubSystem     *pubsub.System
}

type defaultPodGetter struct {
	client client.Client
	scheme *runtime.Scheme
	pod    *corev1.Pod
	mux    sync.RWMutex
}

func (g *defaultPodGetter) GetPod(ctx context.Context) (*corev1.Pod, error) {
	pod := func() *corev1.Pod {
		g.mux.RLock()
		defer g.mux.RUnlock()
		return g.pod
	}()
	if pod != nil {
		return pod.DeepCopy(), nil
	}
	g.mux.Lock()
	defer g.mux.Unlock()
	// guard against the race condition where the pod has been retrieved
	// between releasing the read lock and acquiring the write lock
	if g.pod != nil {
		return g.pod.DeepCopy(), nil
	}
	pod = fakes.Pod(fakes.WithNamespace(util.GetNamespace()),
		fakes.WithName(util.GetPodName()))
	key := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}

	// use unstructured to avoid inadvertently creating a watch on pods
	uPod := &unstructured.Unstructured{}
	gvk, err := apiutil.GVKForObject(pod, g.scheme)
	if err != nil {
		return nil, err
	}
	uPod.SetGroupVersionKind(gvk)
	if err := g.client.Get(ctx, key, uPod); err != nil {
		return nil, err
	}
	if err := g.scheme.Convert(uPod, pod, nil); err != nil {
		return nil, err
	}
	g.pod = pod
	return pod.DeepCopy(), nil
}

// AddToManager adds all Controllers to the Manager.
func AddToManager(m manager.Manager, deps *Dependencies) error {
	if deps.GetPod == nil {
		podGetter := &defaultPodGetter{
			scheme: m.GetScheme(),
			client: m.GetClient(),
		}
		deps.GetPod = podGetter.GetPod
	}
	if *debugUseFakePod {
		err := os.Setenv("POD_NAME", "no-pod")
		if err != nil {
			return err
		}

		fakePodGetter := func(ctx context.Context) (*corev1.Pod, error) {
			pod := fakes.Pod(
				fakes.WithNamespace(util.GetNamespace()),
				fakes.WithName(util.GetPodName()),
			)

			return pod, nil
		}
		deps.GetPod = fakePodGetter
	}

	filteredOpa := syncutil.NewFilteredOpaDataClient(deps.Opa, deps.WatchSet)
	syncMetricsCache := syncutil.NewMetricsCache()
	cm := cm.NewCacheManager(filteredOpa, syncMetricsCache, deps.Tracker, deps.ProcessExcluder)

	for _, a := range Injectors {
		a.InjectOpa(deps.Opa)
		a.InjectWatchManager(deps.WatchManger)
		a.InjectControllerSwitch(deps.ControllerSwitch)
		a.InjectTracker(deps.Tracker)
		a.InjectMutationSystem(deps.MutationSystem)
		a.InjectExpansionSystem(deps.ExpansionSystem)
		a.InjectProviderCache(deps.ProviderCache)
		if a2, ok := a.(GetPodInjector); ok {
			a2.InjectGetPod(deps.GetPod)
		}
		if a2, ok := a.(GetProcessExcluderInjector); ok {
			a2.InjectProcessExcluder(deps.ProcessExcluder)
		}
		if a2, ok := a.(WatchSetInjector); ok {
			a2.InjectWatchSet(deps.WatchSet)
		}
		if a2, ok := a.(PubsubInjector); ok {
			a2.InjectPubsubSystem(deps.PubsubSystem)
		}
		if a2, ok := a.(CacheManagerInjector); ok {
			// this is used by the config controller to sync
			a2.InjectCacheManager(cm)
		}

		if err := a.Add(m); err != nil {
			return err
		}
	}
	for _, f := range AddToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}
	return nil
}
