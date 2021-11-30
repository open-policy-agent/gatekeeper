---
id: externaldata
title: External Data
---

> ❗ This feature is still in alpha stage, so the final form can still change (feedback is welcome!).

> 🚧  Mutation is not yet supported with external data.

## Motivation

Gatekeeper provides various means to mutate and validate Kubernetes resources. However, in many of these scenarios this data is either built-in, static or user defined. With external data feature, we are enabling Gatekeeper to interface with various external data sources, such as image registries, using a provider-based model.

A similar way to connect with an external data source can be done today using OPA's built-in `http.send` functionality. However, there are limitations to this approach.
- Gatekeeper does not support Rego policies for mutation, which cannot use the OPA `http.send` built-in function.
- Security concerns due to:
  - if template authors are not trusted, it will potentially give template authors access to the in-cluster network.
  - if template authors are trusted, authors will need to be careful on how rego is written to avoid injection attacks.

Key benefits provided by the external data solution:
- Addresses security concerns by:
  - Restricting which hosts a user can access.
  - Providing an interface for making requests, which allows Gatekeeper to better handle things like escaping strings.
- Addresses common patterns with a single provider, e.g. image tag-to-digest mutation, which can be leveraged by multiple scenarios (e.g. validating image signatures or vulnerabilities).
- Provider model creates a common interface for extending Gatekeeper with external data.
  - It allows for separation of concerns between the implementation that allows access to external data and the actual policy being evaluated.
  - Developers and consumers of data sources can rely on that common protocol to ease authoring of both constraint templates and data sources.
  - Makes change management easier as users of an external data provider should be able to tell whether upgrading it will break existing constraint templates. (once external data API is stable, our goal is to have that answer always be "no")
- Performance benefits as Gatekeeper can now directly control caching and which values are significant for caching, which increases the likelihood of cache hits.
  - For mutation, we can batch requests via lazy evaluation.
  - For validation, we make batching easier via [`external_data`](#external-data-for-Gatekeeper-validating-webhook) Rego function design.

## Enabling external data support

### YAML
You can enable external data support by adding `--enable-external-data` in gatekeeper audit and controller-manager deployment arguments.

### Helm
You can also enable external data by installing or upgrading Helm chart by setting `enableExternalData=true`:

```sh
helm install gatekeeper/gatekeeper --name-template=gatekeeper --namespace gatekeeper-system --create-namespace \
	--set enableExternalData=true \
	--set controllerManager.dnsPolicy=ClusterFirst,audit.dnsPolicy=ClusterFirst
```

> Please note that setting dnsPolicy is a workaround for a bug that is already fixed and will be available in a future release.

### Dev/Test

For dev/test deployments, use `make deploy ENABLE_EXTERNAL_DATA=true`

## Providers

Providers are designed to be in-cluster components that can communicate with external data sources (such as image registries, Active Directory/LDAP directories, etc) and return data in a format that can be processed by Gatekeeper.

Example provider can be found at: https://github.com/open-policy-agent/gatekeeper/tree/master/test/externaldata/dummy-provider

## Provider implementation

Provider is an HTTP server that listens on a port and responds to [ProviderRequest](#ProviderRequest) with [ProviderResponse](#ProviderResponse).

As part of [ProviderResponse](#ProviderResponse), the provider can return a list of items. Each item is a JSON object with the following fields:
- `Key`: the key that was sent to the provider
- `Value`: the value that was returned from the provider for that key
- `Error`: an error message if the provider returned an error for that key

If there is a system error, the provider should return the system error message in the `SystemError` field.

> 📎 Recommendation is for provider implementations to keep a timeout such as maximum of 1-2 seconds for the provider to respond.

Example provider implementation: https://github.com/open-policy-agent/gatekeeper/blob/master/test/externaldata/dummy-provider/provider.go

## Gatekeeper Provider custom resource

Provider resource defines the provider and the configuration for it.

```yaml
apiVersion: externaldata.gatekeeper.sh/v1alpha1
kind: Provider
metadata:
  name: my-provider
spec:
  url: http://<service-name>.<namespace>:<port>/<endpoint> # URL to the external data source (e.g., http://my-provider.my-namespace:8090/validate)
  timeout: <timeout> # timeout value in seconds (e.g., 1). this is the timeout on the Provider custom resource, not the provider implementation.
```

## External data for Gatekeeper validating webhook

External data adds a [custom OPA built-in function](https://www.openpolicyagent.org/docs/latest/extensions/#custom-built-in-functions-in-go) called `external_data` to Rego. This function is used to query external data providers.

`external_data` is a function that takes a request and returns a response. The request is a JSON object with the following fields:
- `Provider`: the name of the provider to query
- `Keys`: the list of keys to send to the provider

e.g.,
```rego
  # build a list of keys containing images for batching
  my_list := [img | img = input.review.object.spec.template.spec.containers[_].image]

  # send external data request
  response := external_data({"provider": "my-provider", "keys": my_list})
```

Response example: [[`"my-key"`, `"my-value"`, `""`], [`"another-key"`, `42`, `""`], [`"bad-key"`, `""`, `"error message"`]]

> 📎 To avoid multiple calls to the same provider, recommendation is to batch the keys list to send a single request.

Example template:
https://github.com/open-policy-agent/gatekeeper/blob/master/test/externaldata/dummy-provider/policy/template.yaml

## API
### Version v1alpha1
#### ProviderRequest

Request is the API request that is sent to the external data provider.

```go
// ProviderRequest is the API request for the external data provider.
type ProviderRequest struct {
	// APIVersion is the API version of the external data provider.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is kind of the external data provider API call. This can be "ProviderRequest" or "ProviderResponse".
	Kind ProviderKind `json:"kind,omitempty"`
	// Request contains the request for the external data provider.
	Request Request `json:"request,omitempty"`
}

// Request is the struct that contains the keys to query.
type Request struct {
	// Keys is the list of keys to send to the external data provider.
	Keys []string `json:"keys,omitempty"`
}
```

#### ProviderResponse

Response is the API response that a provider must return.

```go
// ProviderResponse is the API response from a provider.
type ProviderResponse struct {
	// APIVersion is the API version of the external data provider.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is kind of the external data provider API call. This can be "ProviderRequest" or "ProviderResponse".
	Kind ProviderKind `json:"kind,omitempty"`
	// Response contains the response from the provider.
	Response Response `json:"response,omitempty"`
}

// Response is the struct that holds the response from a provider.
type Response struct {
	// Idempotent indicates that the responses from the provider are idempotent.
	// Applies to mutation only and must be true for mutation.
	Idempotent bool `json:"idempotent,omitempty"`
	// Items contains the key, value and error from the provider.
	Items []Item `json:"items,omitempty"`
	// SystemError is the system error of the response.
	SystemError string `json:"systemError,omitempty"`
}

// Items is the struct that contains the key, value or error from a provider response.
type Item struct {
	// Key is the request from the provider.
	Key string `json:"key,omitempty"`
	// Value is the response from the provider.
	Value interface{} `json:"value,omitempty"`
	// Error is the error from the provider.
	Error string `json:"error,omitempty"`
}
```