package externaldata

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	"k8s.io/apimachinery/pkg/util/wait"
)

type ProviderCache struct {
	cache map[string]unversioned.Provider
	mux   sync.RWMutex
}

type ProviderResponseCache struct {
	cache sync.Map
	TTL   time.Duration
}

type CacheKey struct {
	ProviderName string
	Key          string
}

type CacheValue struct {
	Received   int64
	Value      interface{}
	Error      string
	Idempotent bool
}

func NewProviderResponseCache(ctx context.Context, ttl time.Duration) *ProviderResponseCache {
	providerResponseCache := &ProviderResponseCache{
		cache: sync.Map{},
		TTL:   ttl,
	}

	go wait.UntilWithContext(ctx, func(_ context.Context) {
		providerResponseCache.invalidateProviderResponseCache(providerResponseCache.TTL)
	}, ttl)

	return providerResponseCache
}

func (c *ProviderResponseCache) Get(key CacheKey) (*CacheValue, error) {
	if v, ok := c.cache.Load(key); ok {
		value, ok := v.(*CacheValue)
		if !ok {
			return nil, fmt.Errorf("value is not of type CacheValue")
		}
		return value, nil
	}
	return nil, fmt.Errorf("key '%s:%s' is not found in provider response cache", key.ProviderName, key.Key)
}

func (c *ProviderResponseCache) Upsert(key CacheKey, value CacheValue) {
	c.cache.Store(key, &value)
}

func (c *ProviderResponseCache) Remove(key CacheKey) {
	c.cache.Delete(key)
}

func (c *ProviderResponseCache) invalidateProviderResponseCache(ttl time.Duration) {
	c.cache.Range(func(k, v interface{}) bool {
		value, ok := v.(*CacheValue)
		if !ok {
			return false
		}

		if time.Since(time.Unix(value.Received, 0)) > ttl {
			key, ok := k.(CacheKey)
			if !ok {
				return false
			}
			c.Remove(key)
		}
		return true
	})
}

func NewCache() *ProviderCache {
	return &ProviderCache{
		cache: make(map[string]unversioned.Provider),
	}
}

func (c *ProviderCache) Get(key string) (unversioned.Provider, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	if v, ok := c.cache[key]; ok {
		dc := *v.DeepCopy()
		return dc, nil
	}
	return unversioned.Provider{}, fmt.Errorf("key is not found in provider cache")
}

func (c *ProviderCache) Upsert(provider *unversioned.Provider) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	if !isValidName(provider.Name) {
		return fmt.Errorf("provider name can not be empty. value %s", provider.Name)
	}
	if !isValidURL(provider.Spec.URL) {
		return fmt.Errorf("invalid provider url. value: %s", provider.Spec.URL)
	}
	if !isValidTimeout(provider.Spec.Timeout) {
		return fmt.Errorf("provider timeout should be a positive integer. value: %d", provider.Spec.Timeout)
	}
	if err := isValidCABundle(provider); err != nil {
		return err
	}

	c.cache[provider.GetName()] = *provider.DeepCopy()
	return nil
}

func (c *ProviderCache) Remove(name string) {
	c.mux.Lock()
	defer c.mux.Unlock()

	delete(c.cache, name)
}

func isValidName(name string) bool {
	return len(name) != 0
}

func isValidURL(url string) bool {
	return len(url) != 0
}

func isValidTimeout(timeout int) bool {
	return timeout >= 0
}

func isValidCABundle(provider *unversioned.Provider) error {
	// verify attempts to parse the caBundle as a PEM encoded certificate
	// to make sure it is valid before adding it to the cache
	verify := func(caBundle string) error {
		caPem, err := base64.StdEncoding.DecodeString(caBundle)
		if err != nil {
			return fmt.Errorf("failed to base64 decode caBundle: %w", err)
		}

		caDer, _ := pem.Decode(caPem)
		if caDer == nil {
			return fmt.Errorf("bad caBundle")
		}

		_, err = x509.ParseCertificate(caDer.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse caBundle: %w", err)
		}

		return nil
	}

	u, err := url.Parse(provider.Spec.URL)
	if err != nil {
		return err
	}

	switch u.Scheme {
	case HTTPSScheme:
		if provider.Spec.CABundle == "" {
			return fmt.Errorf("caBundle should be set for HTTPS scheme")
		}
		if err := verify(provider.Spec.CABundle); err != nil {
			return err
		}
	default:
		return fmt.Errorf("only HTTPS scheme is supported for Providers")
	}

	return nil
}
