package gator

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
)

// WithExternalDataProviders enables external_data with only the given
// Providers. Pass it at most once; a second call replaces the first.
func WithExternalDataProviders(providers ...*unversioned.Provider) Opt {
	return func() ([]constraintclient.Opt, []rego.Arg, error) {
		if len(providers) == 0 {
			return nil, nil, fmt.Errorf("no external data Providers supplied")
		}
		cache := externaldata.NewCache()
		seen := make(map[string]bool, len(providers))
		for _, provider := range providers {
			if provider == nil {
				return nil, nil, fmt.Errorf("nil external data Provider")
			}
			if seen[provider.Name] {
				return nil, nil, fmt.Errorf("provider %q is defined more than once", provider.Name)
			}
			seen[provider.Name] = true
			if err := addProvider(cache, provider); err != nil {
				return nil, nil, fmt.Errorf("invalid Provider %q: %w", provider.Name, err)
			}
		}
		// Setting a Provider cache re-enables external_data, overriding the
		// DisableBuiltins default in gator clients.
		return nil, []rego.Arg{rego.AddExternalDataProviderCache(cache)}, nil
	}
}

// ValidateProvider applies the same checks as WithExternalDataProviders.
func ValidateProvider(provider *unversioned.Provider) error {
	return addProvider(externaldata.NewCache(), provider)
}

// addProvider adds provider to cache. Unlike the cluster, it also rejects a
// zero timeout, which makes every request time out.
func addProvider(cache *externaldata.ProviderCache, provider *unversioned.Provider) error {
	if provider == nil {
		return fmt.Errorf("nil external data Provider")
	}
	if err := cache.Upsert(provider); err != nil {
		return err
	}
	if provider.Spec.Timeout <= 0 {
		return fmt.Errorf("spec.timeout must be a positive number of seconds")
	}
	return nil
}
