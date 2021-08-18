package externaldata

import (
	"fmt"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
)

type ProviderCache struct {
	cache map[string]v1alpha1.Provider
	mux   sync.RWMutex
}

func NewCache() *ProviderCache {
	return &ProviderCache{
		cache: make(map[string]v1alpha1.Provider),
	}
}

func (c *ProviderCache) Get(key string) (v1alpha1.Provider, error) {
	if v, ok := c.cache[key]; ok {
		return v, nil
	}
	return v1alpha1.Provider{}, fmt.Errorf("key is not found in provider cache")
}

func (c *ProviderCache) Upsert(provider *v1alpha1.Provider) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.cache[provider.GetName()] = v1alpha1.Provider{
		Spec: v1alpha1.ProviderSpec{
			ProxyURL:      provider.Spec.ProxyURL,
			FailurePolicy: provider.Spec.FailurePolicy,
			Timeout:       provider.Spec.Timeout,
			MaxRetry:      provider.Spec.MaxRetry,
		},
	}

	return nil
}

func (c *ProviderCache) Remove(name string) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	delete(c.cache, name)

	return nil
}
