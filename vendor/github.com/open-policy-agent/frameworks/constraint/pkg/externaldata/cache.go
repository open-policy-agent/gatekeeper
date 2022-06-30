package externaldata

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
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
	c.mux.RLock()
	defer c.mux.RUnlock()

	if v, ok := c.cache[key]; ok {
		dc := *v.DeepCopy()
		return dc, nil
	}
	return v1alpha1.Provider{}, fmt.Errorf("key is not found in provider cache")
}

func (c *ProviderCache) Upsert(provider *v1alpha1.Provider) error {
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

func isValidCABundle(provider *v1alpha1.Provider) error {
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
	case HTTPScheme:
		if !provider.Spec.InsecureTLSSkipVerify {
			return fmt.Errorf("only HTTPS scheme is supported for this Provider. To enable HTTP scheme, set insecureTLSSkipVerify to true")
		}
		if provider.Spec.CABundle != "" {
			if err := verify(provider.Spec.CABundle); err != nil {
				return err
			}
		}
	case HTTPSScheme:
		if provider.Spec.CABundle == "" {
			return fmt.Errorf("caBundle should be set for HTTPS scheme")
		}
		if err := verify(provider.Spec.CABundle); err != nil {
			return err
		}
	default:
		return fmt.Errorf("only HTTPS and HTTP schemes are supported for Providers")
	}

	return nil
}
