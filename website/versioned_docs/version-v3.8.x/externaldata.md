---
id: externaldata
title: External Data
---

`Feature State`: Gatekeeper version v3.7+ (alpha)

> ❗ This feature is still in alpha stage, so the final form can still change (feedback is welcome!).

> ✅  Mutation is supported with external data starting from v3.8.0.

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
	--set enableExternalData=true
```

### Dev/Test

For dev/test deployments, use `make deploy ENABLE_EXTERNAL_DATA=true`

## Providers

Providers are designed to be in-cluster components that can communicate with external data sources (such as image registries, Active Directory/LDAP directories, etc) and return data in a format that can be processed by Gatekeeper.

Example provider _template_ can be found at: https://github.com/open-policy-agent/gatekeeper-external-data-provider

### Providers maintained by the community

If you have built an external data provider and would like to add it to this list, please submit a PR to update this page.

If you have any issues with a specific provider, please open an issue in the applicable provider's repository.

The following external data providers are maintained by the community:

- [ratify](https://github.com/deislabs/ratify)
- [cosign-gatekeeper-provider](https://github.com/sigstore/cosign-gatekeeper-provider)

### Sample providers

The following external data providers are samples and are not supported/maintained by the community:

- [trivy-provider](https://github.com/sozercan/trivy-provider)
- [tag-to-digest-provider](https://github.com/sozercan/tagToDigest-provider)
- [aad-provider](https://github.com/sozercan/aad-provider)

### API (v1alpha1)

#### `Provider`

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

#### `ProviderRequest`

`ProviderRequest` is the API request that is sent to the external data provider.

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

#### `ProviderResponse`

`ProviderResponse` is the API response that a provider must return.

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

### Implementation

Provider is an HTTP server that listens on a port and responds to [`ProviderRequest`](#providerrequest) with [`ProviderResponse`](#providerresponse).

As part of [`ProviderResponse`](#providerresponse), the provider can return a list of items. Each item is a JSON object with the following fields:
- `Key`: the key that was sent to the provider
- `Value`: the value that was returned from the provider for that key
- `Error`: an error message if the provider returned an error for that key

If there is a system error, the provider should return the system error message in the `SystemError` field.

> 📎 Recommendation is for provider implementations to keep a timeout such as maximum of 1-2 seconds for the provider to respond.

Example provider implementation: https://github.com/open-policy-agent/gatekeeper/blob/master/test/externaldata/dummy-provider/provider.go

## External data for Gatekeeper validating webhook

External data adds a [custom OPA built-in function](https://www.openpolicyagent.org/docs/latest/extensions/#custom-built-in-functions-in-go) called `external_data` to Rego. This function is used to query external data providers.

`external_data` is a function that takes a request and returns a response. The request is a JSON object with the following fields:
- `provider`: the name of the provider to query
- `keys`: the list of keys to send to the provider

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

## External data for Gatekeeper mutating webhook

External data can be used in conjunction with [Gatekeeper mutating webhook](mutation.md).

### API

You can specify the details of the external data provider in the `spec.parameters.assign.externalData` field of `AssignMetadata` and `Assign`.

> Note: `spec.parameters.assign.externalData`, `spec.parameters.assign.value` and `spec.parameters.assign.fromMetadata` are mutually exclusive.

| Field                            | Description                                                                                                                                                 |
|----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `provider`<br/>String             | The name of the external data provider.                                                                                                                     |
| `dataSource`<br/>DataSource       | Specifies where to extract the data that will be sent to the external data provider.<br/>- `ValueAtLocation` (default): extracts an array of values from the path that will be modified. See [mutation intent](mutation.md#intent) for more details.<br/>- `Username`: The name of the Kubernetes user who initiated the admission request.  |
| `failurePolicy`<br/>FailurePolicy | The policy to apply when the external data provider returns an error.<br/>- `UseDefault`: use the default value specified in `spec.parameters.assign.externalData.default`<br/>- `Ignore`: ignore the error and do not perform any mutations.<br/>- `Fail` (default): do not perform any mutations and return the error to the user.                                       |
| `default`<br/>String              | The default value to use when the external data provider returns an error and the failure policy is set to `UseDefault`.                                    |

### `AssignMetadata`

```yaml
apiVersion: mutations.gatekeeper.sh/v1beta1
kind: AssignMetadata
metadata:
  name: annotate-owner
spec:
  match:
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
  location: "metadata.annotations.owner"
  parameters:
    assign:
      externalData:
        provider: my-provider
        dataSource: Username
```

<details>
<summary>Provider response</summary>

```json
{
  "apiVersion": "externaldata.gatekeeper.sh/v1alpha1",
  "kind": "ProviderResponse",
  "response": {
    "idempotent": true,
    "items": [
      {
        "key": "kubernetes-admin",
        "value": "admin@example.com"
      }
    ]
  }
}
```

</details>

<details>
<summary>Mutated object</summary>

```yaml
...
metadata:
  annotations:
    owner: admin@example.com
...
```

</details>

### `Assign`

```yaml
apiVersion: mutations.gatekeeper.sh/v1beta1
kind: Assign
metadata:
  name: mutate-images
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  match:
    scope: Namespaced
  location: "spec.containers[name:*].image"
  parameters:
    assign:
      externalData:
        provider: my-provider
        dataSource: ValueAtLocation
        failurePolicy: UseDefault
        default: busybox:latest
```

<details>
<summary>Provider response</summary>

```json
{
  "apiVersion": "externaldata.gatekeeper.sh/v1alpha1",
  "kind": "ProviderResponse",
  "response": {
    "idempotent": true,
    "items": [
      {
        "key": "nginx",
        "value": "nginx:v1.2.3"
      }
    ]
  }
}
```

</details>

<details>
<summary>Mutated object</summary>

```yaml
...
spec:
  containers:
    - name: nginx
      image: nginx:v1.2.3
...
```

</details>

### Limitations

There are several limitations when using external data with the mutating webhook:

- Only supports mutation of `string` fields (e.g. `.spec.containers[name:*].image`).
- `AssignMetadata` only supports `dataSource: Username` because `AssignMetadata` only supports creation of `metadata.annotations` and `metadata.labels`. `dataSource: ValueAtLocation` will not return any data.
- `ModifySet` does not support external data.
- Multiple mutations to the same object are applied alphabetically based on the name of the mutation CRDs. If you have an external data mutation and a non-external data mutation with the same `spec.location`, the final result might not be what you expected. Currently, there is no way to enforce custom ordering of mutations but the issue is being tracked [here](https://github.com/open-policy-agent/gatekeeper/issues/1133).

## Authenticate the API server against Webhook (Self managed K8s cluster only)

**Note:** To enable authenticating the API server you have to be able to modify cluster resources. This may not be possible for managed K8s clusters.

To ensure a request to the Gatekeeper webhook is coming from the API server, Gatekeeper needs to validate the client cert in the request. To enable authenticate API server, the following configuration can be made:

1. Deploy Gatekeeper with a client CA cert name. Provide name of the client CA with the flag `--client-ca-name`. The same name will be used to read certificate from the webhook secret. The webhook will only authenticate API server requests if client CA name is provided with flag.

2. You will need to patch the webhook secret manually to attach client ca crt. Update secret `gatekeeper-webhook-server-cert` to include `clientca.crt`. Key name `clientca.crt` should match the name passed with `--client-ca-name` flag. You could generate your own CA for this purpose.

    ```
    kind: Secret
    apiVersion: v1
    data:
      ca.crt: <ca-crt>
      ca.key: <ca-key>
      tls.crt: <tls-crt>
      tls.key: <tls-key>
      clientca.crt: <ca-crt>
    metadata:
      ...
      name: <gatekeeper-webhook-service-name>
      namespace: <gatekeeper-namespace>
    type: Opaque
    ```
3. Generate CA and client certificates signed with CA authority to be attached by the API server while talking to Gatekeeper webhook. Gatekeeper webhook expects the API server to attach the certificate that has CN name as `kube-apiserver`. Use `--client-cn-name` to provide custom cert CN name if using a certificate with other CN name, otherwise webhook will throw the error and not accept the request.

    - Generate private key for CA
    
      ```bash      
        openssl genrsa -des3 -out myCA.key 2048
      ```
    
    - Generate a root certificate

    ```bash
      openssl req -x509 -new -nodes -key myCA.key -sha256 -days 1825 -out myCA.crt
    ```

    - Generate private key for API server

      ```bash
      openssl genrsa -out apiserver-client.key 2048
      ```
    - Generate a CSR for API server
    
      ```bash
      openssl req -new -key apiserver-client.key -out apiserver-client.csr
      ```
    
    - Generate public key for API server
      
      ```bash
      openssl x509 -req -in apiserver-client.csr -CA myCA.crt -CAkey myCA.key -CAcreateserial -out apiserver-client.crt -days 365
      ```
      | The client certificate generated by the above command will expire in 365 days, and you must renew it before it expires. Adjust the value of `-day` to set the expiry on the client certificate according to your need.

4. You will need to make sure the K8s API Server includes the appropriate certificate while sending requests to the webhook, otherwise webhook will not accept these requests and will log an error of `tls client didn't provide a certificate`. To make sure the API server attaches the correct certificate to requests being sent to webhook, you must specify the location of the admission control configuration file via the `--admission-control-config-file` flag while starting the API server. Here is an example admission control configuration file:
    ```
    apiVersion: apiserver.config.k8s.io/v1
    kind: AdmissionConfiguration
    plugins:
    - name: ValidatingAdmissionWebhook
      configuration:
        apiVersion: apiserver.config.k8s.io/v1
        kind: WebhookAdmissionConfiguration
        kubeConfigFile: "<path-to-kubeconfig-file>"
    - name: MutatingAdmissionWebhook
      configuration:
        apiVersion: apiserver.config.k8s.io/v1
        kind: WebhookAdmissionConfiguration
        kubeConfigFile: "<path-to-kubeconfig-file>"
    ```
  
    KubeConfig file should look something like:

    ```
    apiVersion: v1
    clusters:
    - cluster:
        certificate-authority-data: <ca-crt> # same value as provided in gatekeeper webhook secret's clientca.crt 
        server: https://<gatekeeper-webhook-service-name>.<gatekeeper-namespace>.svc:443
      name: <cluster-name>
    contexts:
    - context:
        cluster: <cluster-name>
        user: api-server
      name: <context-name>
    current-context: <context-name>
    kind: Config
    users:
    - name: api-server
      user:
        client-certificate-data: <apiserver-client.crt> 
        client-key-data: <apiserver-client.key>
    ```
    **Note**: Default `gatekeeper-webhook-service-name` is `gatekeeper-webhook-service` and default `gatekeeper-namespace` is `gatekeeper-system`.

    Visit [#authenticate-apiservers](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#authenticate-apiservers) for more details.