package rego

import (
	"net/http"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
)

const (
	providerResponseAPIVersion = "externaldata.gatekeeper.sh/v1beta1"
	providerResponseKind       = "ProviderResponse"
)

func externalDataBuiltin(d *Driver) func(bctx rego.BuiltinContext, regorequest *ast.Term) (*ast.Term, error) {
	return func(bctx rego.BuiltinContext, regorequest *ast.Term) (*ast.Term, error) {
		var regoReq externaldata.RegoRequest
		if err := ast.As(regorequest.Value, &regoReq); err != nil {
			return nil, err
		}

		provider, err := d.providerCache.Get(regoReq.ProviderName)
		if err != nil {
			return externaldata.HandleError(http.StatusBadRequest, err)
		}

		clientCert, err := d.getTLSCertificate()
		if err != nil {
			return externaldata.HandleError(http.StatusBadRequest, err)
		}

		// check provider response cache
		var providerRequestKeys []string
		var providerResponseStatusCode int
		var prepareResponse externaldata.Response

		prepareResponse.Idempotent = true
		for _, k := range regoReq.Keys {
			if d.providerResponseCache == nil {
				// external data response cache is not enabled, add key to call provider
				providerRequestKeys = append(providerRequestKeys, k)
				continue
			}

			cachedResponse, err := d.providerResponseCache.Get(
				externaldata.CacheKey{
					ProviderName: regoReq.ProviderName,
					Key:          k,
				},
			)
			if err != nil || time.Since(time.Unix(cachedResponse.Received, 0)) > d.providerResponseCache.TTL {
				// key is not found or cache entry is stale, add key to the provider request keys
				providerRequestKeys = append(providerRequestKeys, k)
			} else {
				prepareResponse.Items = append(
					prepareResponse.Items, externaldata.Item{
						Key:   k,
						Value: cachedResponse.Value,
						Error: cachedResponse.Error,
					},
				)

				// we are taking conservative approach here, if any of the cached response is not idempotent
				// we will mark the whole response as not idempotent
				if !cachedResponse.Idempotent {
					prepareResponse.Idempotent = false
				}
			}
		}

		if len(providerRequestKeys) > 0 {
			externaldataResponse, statusCode, err := d.sendRequestToProvider(bctx.Context, &provider, providerRequestKeys, clientCert)
			if err != nil {
				return externaldata.HandleError(statusCode, err)
			}

			// update provider response cache if it is enabled
			if d.providerResponseCache != nil {
				for _, item := range externaldataResponse.Response.Items {
					d.providerResponseCache.Upsert(
						externaldata.CacheKey{
							ProviderName: regoReq.ProviderName,
							Key:          item.Key,
						},
						externaldata.CacheValue{
							Received:   time.Now().Unix(),
							Value:      item.Value,
							Error:      item.Error,
							Idempotent: externaldataResponse.Response.Idempotent,
						},
					)
				}
			}

			// we are taking conservative approach here, if any of the response is not idempotent
			// we will mark the whole response as not idempotent
			if !externaldataResponse.Response.Idempotent {
				prepareResponse.Idempotent = false
			}

			prepareResponse.Items = append(prepareResponse.Items, externaldataResponse.Response.Items...)
			prepareResponse.SystemError = externaldataResponse.Response.SystemError
			providerResponseStatusCode = statusCode
		}

		providerResponse := &externaldata.ProviderResponse{
			APIVersion: providerResponseAPIVersion,
			Kind:       providerResponseKind,
			Response:   prepareResponse,
		}

		regoResponse := externaldata.NewRegoResponse(providerResponseStatusCode, providerResponse)
		return externaldata.PrepareRegoResponse(regoResponse)
	}
}
