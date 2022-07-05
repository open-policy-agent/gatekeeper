package target

import (
	"fmt"
	"strings"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type nsCache struct {
	lock  sync.RWMutex
	cache map[string]*corev1.Namespace
}

var _ handler.Cache = &nsCache{}

func (c *nsCache) Add(key []string, object interface{}) error {
	obj, ok := object.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%w: cannot cache type %T, want %T", ErrCachingType, object, map[string]interface{}{})
	}

	u := &unstructured.Unstructured{Object: obj}

	nsType := schema.GroupKind{Kind: "Namespace"}
	if u.GroupVersionKind().GroupKind() != nsType {
		return nil
	}

	ns, err := toNamespace(u)
	if err != nil {
		return fmt.Errorf("%w: cannot cache Namespace: %v", ErrCachingType, ns)
	}

	c.AddNamespace(toKey(key), ns)

	return nil
}

func (c *nsCache) AddNamespace(key string, ns *corev1.Namespace) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.cache == nil {
		c.cache = make(map[string]*corev1.Namespace)
	}

	c.cache[key] = ns
}

func (c *nsCache) GetNamespace(name string) *corev1.Namespace {
	key := clusterScopedKey(corev1.SchemeGroupVersion.WithKind("Namespace"), name)

	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.cache[toKey(key)]
}

func (c *nsCache) Remove(key []string) {
	c.RemoveNamespace(toKey(key))
}

func (c *nsCache) RemoveNamespace(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.cache, key)
}

func toKey(parts []string) string {
	return strings.Join(parts, "/")
}

func toNamespace(u *unstructured.Unstructured) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, ns)
	if err != nil {
		return nil, err
	}

	return ns, nil
}
