package mutation

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
)

// resolvePlaceholders resolves all external data placeholders in the given object.
func (s *System) resolvePlaceholders(ctx context.Context, obj *unstructured.Unstructured) error {
	providerKeys := make(map[string]sets.Set[string])

	// recurse object to find all existing external data placeholders
	var recurse func(object interface{})
	recurse = func(object interface{}) {
		switch obj := object.(type) {
		case *unversioned.ExternalDataPlaceholder:
			if _, ok := providerKeys[obj.Ref.Provider]; !ok {
				providerKeys[obj.Ref.Provider] = sets.New[string]()
			}
			// gather and de-duplicate all keys for this
			// provider so we can resolve them in batch
			providerKeys[obj.Ref.Provider].Insert(obj.ValueAtLocation)
			return
		case map[string]interface{}:
			for _, v := range obj {
				recurse(v)
			}
		case []interface{}:
			for _, item := range obj {
				recurse(item)
			}
		}
	}

	recurse(obj.Object)
	if len(providerKeys) == 0 {
		return nil
	}

	cachedExternalData, providerKeys := s.cachedProviderResponses(providerKeys)
	if len(providerKeys) == 0 {
		return s.mutateWithExternalData(obj, cachedExternalData, nil)
	}

	clientCert, err := s.getTLSCertificate()
	if err != nil {
		return fmt.Errorf("failed to get client TLS certificate: %w", err)
	}

	externalData, errors := s.sendRequests(ctx, providerKeys, clientCert)
	externalData = mergeExternalData(cachedExternalData, externalData)
	return s.mutateWithExternalData(obj, externalData, errors)
}

func (s *System) cachedProviderResponses(providerKeys map[string]sets.Set[string]) (map[string]map[string]*externaldata.Item, map[string]sets.Set[string]) {
	if s.providerCache == nil || s.providerResponseCache == nil || s.providerResponseCache.TTL <= 0 {
		return nil, providerKeys
	}

	cached := make(map[string]map[string]*externaldata.Item)
	misses := make(map[string]sets.Set[string])
	for providerName, keys := range providerKeys {
		provider, err := s.providerCache.Get(providerName)
		if err != nil {
			misses[providerName] = keys
			continue
		}
		for key := range keys {
			cacheValue, err := s.providerResponseCache.Get(mutationProviderResponseCacheKey(&provider, key))
			if err != nil || cacheValue == nil || !cacheValue.Idempotent || time.Since(time.Unix(cacheValue.Received, 0)) > s.providerResponseCache.TTL {
				if _, ok := misses[providerName]; !ok {
					misses[providerName] = sets.New[string]()
				}
				misses[providerName].Insert(key)
				continue
			}

			if _, ok := cached[providerName]; !ok {
				cached[providerName] = make(map[string]*externaldata.Item)
			}
			item := externaldata.Item{
				Key:   key,
				Value: cacheValue.Value,
				Error: cacheValue.Error,
			}
			cached[providerName][key] = &item
		}
	}

	return cached, misses
}

func mutationProviderResponseCacheKey(provider *externaldataUnversioned.Provider, key string) externaldata.CacheKey {
	return externaldata.CacheKey{
		ProviderName: fmt.Sprintf("%s/%s/%d", provider.GetName(), provider.GetUID(), provider.GetGeneration()),
		Key:          key,
	}
}

func mergeExternalData(cached, fresh map[string]map[string]*externaldata.Item) map[string]map[string]*externaldata.Item {
	if len(cached) == 0 {
		return fresh
	}
	if len(fresh) == 0 {
		return cached
	}

	for providerName, items := range fresh {
		if _, ok := cached[providerName]; !ok {
			cached[providerName] = items
			continue
		}
		for key, item := range items {
			cached[providerName][key] = item
		}
	}
	return cached
}

const defaultExternalDataRequestTimeout = 5 * time.Second

// sendRequests sends requests to all providers in parallel.
func (s *System) sendRequests(ctx context.Context, providerKeys map[string]sets.Set[string], clientCert *tls.Certificate) (map[string]map[string]*externaldata.Item, map[string]error) {
	var (
		wg    sync.WaitGroup
		mutex sync.RWMutex
		fn    = s.sendRequestToExternalDataProvider

		// the provider name is the first key and the outbound data is the second key
		responses = make(map[string]map[string]*externaldata.Item)
		// errors that might have occurred per provider
		errors = make(map[string]error)
	)

	if fn == nil {
		fn = externaldata.DefaultSendRequestToProvider
	}

	for name, keys := range providerKeys {
		provider, err := s.providerCache.Get(name)
		if err != nil {
			log.Error(err, "failed to get external data provider", "provider", name)
			errors[name] = fmt.Errorf("failed to get external data provider %s: %w", name, err)
			continue
		}

		providerCopy := provider
		keysList := keys.UnsortedList()

		wg.Add(1)
		go func(provider externaldataUnversioned.Provider, keys []string) {
			defer wg.Done()

			timeout := time.Duration(provider.Spec.Timeout) * time.Second
			if timeout <= 0 {
				timeout = defaultExternalDataRequestTimeout
			}
			reqCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			resp, _, err := fn(reqCtx, &provider, keys, clientCert)

			mutex.Lock()
			defer mutex.Unlock()

			if err != nil {
				setProviderKeyErrors(responses, provider.Name, keys, fmt.Errorf("failed to send external data request to provider %s: %w", provider.Name, err))
				return
			}
			if err := validateExternalDataResponse(resp); err != nil {
				setProviderKeyErrors(responses, provider.Name, keys, fmt.Errorf("failed to validate external data response from provider %s: %w", provider.Name, err))
				return
			}

			responses[provider.Name] = make(map[string]*externaldata.Item)
			requestedKeys := sets.New(keys...)
			received := time.Now().Unix()
			for _, item := range resp.Response.Items {
				if !requestedKeys.Has(item.Key) {
					continue
				}
				itemCopy := item
				responses[provider.Name][item.Key] = &itemCopy
				if s.providerResponseCache != nil && s.providerResponseCache.TTL > 0 {
					s.providerResponseCache.Upsert(
						mutationProviderResponseCacheKey(&provider, item.Key),
						externaldata.CacheValue{
							Received:   received,
							Value:      item.Value,
							Error:      item.Error,
							Idempotent: resp.Response.Idempotent,
						},
					)
				}
			}
		}(providerCopy, keysList)
	}
	wg.Wait()

	return responses, errors
}

func setProviderKeyErrors(responses map[string]map[string]*externaldata.Item, providerName string, keys []string, err error) {
	if _, ok := responses[providerName]; !ok {
		responses[providerName] = make(map[string]*externaldata.Item, len(keys))
	}
	for _, key := range keys {
		responses[providerName][key] = &externaldata.Item{Key: key, Error: err.Error()}
	}
}

// mutateWithExternalData recursively traverses the given object and replaces
// all external data placeholders with the corresponding external data items.
func (s *System) mutateWithExternalData(object *unstructured.Unstructured, externalData map[string]map[string]*externaldata.Item, errors map[string]error) error {
	var mutate func(interface{}) []error
	mutate = func(current interface{}) []error {
		var allErrors []error
		switch obj := current.(type) {
		case map[string]interface{}:
			for k, v := range obj {
				placeholder, ok := v.(*unversioned.ExternalDataPlaceholder)
				if !ok {
					// not a placeholder, let's continue recursing
					allErrors = append(allErrors, mutate(v)...)
					continue
				}

				// our base case - we found a placeholder and we should resolve it
				var data *externaldata.Item
				var providerResponse map[string]*externaldata.Item

				err := errors[placeholder.Ref.Provider]
				if err == nil {
					providerResponse, ok = externalData[placeholder.Ref.Provider]
					if !ok {
						err = fmt.Errorf("failed to find external data provider %s in responses", placeholder.Ref.Provider)
					}
				}

				if err == nil && providerResponse != nil {
					// we expect the response to contain the key we're looking for
					data, ok = providerResponse[placeholder.ValueAtLocation]
					if !ok {
						err = fmt.Errorf("failed to find external data item in response for provider %s", placeholder.Ref.Provider)
					}
				}

				var valueAsString string
				if err == nil && data != nil {
					value := data.Value
					if data.Error != "" {
						err = fmt.Errorf("failed to retrieve external data item from provider %s: %s", placeholder.Ref.Provider, data.Error)
					} else if valueAsString, ok = value.(string); !ok {
						err = fmt.Errorf("failed to convert external data item value from provider %s to string, got type %T", placeholder.Ref.Provider, value)
					}
				}

				if err != nil {
					switch placeholder.Ref.FailurePolicy {
					case types.FailurePolicyFail:
						allErrors = append(allErrors, err)
						continue
					case types.FailurePolicyIgnore:
						log.Error(fmt.Errorf("%w. Ignoring and using placeholder value", err), "key", placeholder.ValueAtLocation)
						valueAsString = placeholder.ValueAtLocation
					case types.FailurePolicyUseDefault:
						defaultValue := placeholder.Ref.Default
						log.Error(fmt.Errorf("%w. Ignoring and using default value", err), "key", placeholder.ValueAtLocation, "default", defaultValue)
						valueAsString = defaultValue
					}
				}

				obj[k] = valueAsString
			}
		case []interface{}:
			for _, item := range obj {
				allErrors = append(allErrors, mutate(item)...)
			}
		}

		return allErrors
	}

	return errorsutil.NewAggregate(mutate(object.Object))
}

// getTLSCertificate returns the gatekeeper's TLS certificate.
func (s *System) getTLSCertificate() (*tls.Certificate, error) {
	if s.clientCertWatcher == nil {
		return nil, fmt.Errorf("external data client certificate watcher is not initialized")
	}

	return s.clientCertWatcher.GetCertificate(nil)
}

// validateExternalDataResponse validates the given external data response.
func validateExternalDataResponse(r *externaldata.ProviderResponse) error {
	if systemError := strings.TrimSpace(r.Response.SystemError); systemError != "" {
		return fmt.Errorf("non-empty system error: %s", systemError)
	}

	if !r.Response.Idempotent {
		return fmt.Errorf("non-idempotent response")
	}

	return nil
}
