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

package readiness_test

import (
	"context"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	ctrl_cache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type mockManager struct {
	mockCache *mockCache
}

func (m *mockManager) Add(runnable manager.Runnable) error {
	return nil
}
func (m *mockManager) Elected() <-chan struct{} {
	return nil
}

func (m *mockManager) SetFields(fields interface{}) error {
	return nil
}

func (m *mockManager) AddMetricsExtraHandler(path string, handler http.Handler) error {
	return nil
}

func (m *mockManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return nil
}

func (m *mockManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return nil
}

func (m *mockManager) Start(<-chan struct{}) error {
	return nil
}

func (m *mockManager) GetConfig() *rest.Config {
	return nil
}

func (m *mockManager) GetScheme() *runtime.Scheme {
	return nil
}

func (m *mockManager) GetClient() client.Client {
	return nil
}

func (m *mockManager) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (m *mockManager) GetCache() ctrl_cache.Cache {
	return m.mockCache
}

func (m *mockManager) GetEventRecorderFor(name string) record.EventRecorder {
	return nil
}

func (m *mockManager) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (m *mockManager) GetAPIReader() client.Reader {
	return nil
}
func (m *mockManager) GetWebhookServer() *webhook.Server {
	return nil
}

type mockCache struct {
	informers              map[schema.GroupVersionKind]*mockInformer
	waitForCacheSyncCalled bool
}

func (m *mockCache) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	return nil
}
func (m *mockCache) List(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
	return nil
}

func (m *mockCache) GetInformer(ctx context.Context, obj runtime.Object) (ctrl_cache.Informer, error) {
	return m.GetInformerForKind(ctx, obj.GetObjectKind().GroupVersionKind())
}

func (m *mockCache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind) (ctrl_cache.Informer, error) {
	return m.informers[gvk], nil
}
func (m *mockCache) Start(stopCh <-chan struct{}) error {
	return nil
}
func (m *mockCache) WaitForCacheSync(stop <-chan struct{}) bool {
	m.waitForCacheSyncCalled = true
	return true
}
func (m *mockCache) IndexField(ctx context.Context, obj runtime.Object, field string, extractValue client.IndexerFunc) error {
	return nil
}

type mockInformer struct {
	mockStore       *cache.Store
	hasSyncedCalled bool
}

func (m *mockInformer) AddEventHandler(handler cache.ResourceEventHandler) {}
func (m *mockInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) {
}
func (m *mockInformer) AddIndexers(indexers cache.Indexers) error {
	return nil
}

func (m *mockInformer) GetStore() cache.Store {
	return *m.mockStore
}
func (m *mockInformer) GetController() cache.Controller {
	return nil
}
func (m *mockInformer) Run(stopCh <-chan struct{}) {}
func (m *mockInformer) HasSynced() bool {
	m.hasSyncedCalled = true
	return true
}
func (m *mockInformer) LastSyncResourceVersion() string {
	return ""
}

type mockStore struct {
}

func (m *mockStore) Add(obj interface{}) error {
	return nil
}
func (m *mockStore) Update(obj interface{}) error {
	return nil
}
func (m *mockStore) Delete(obj interface{}) error {
	return nil
}
func (m *mockStore) List() []interface{} {
	return nil
}
func (m *mockStore) ListKeys() []string {
	return nil
}
func (m *mockStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	return nil, true, nil
}
func (m *mockStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	return nil, true, nil
}

func (m *mockStore) Replace([]interface{}, string) error {
	return nil
}
func (m *mockStore) Resync() error {
	return nil
}
