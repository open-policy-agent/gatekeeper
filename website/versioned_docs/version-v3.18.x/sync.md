---
id: sync
title: Replicating Data
---

## Replicating Data

Some constraints are impossible to write without access to more state than just the object under test. For example, it is impossible to know if a label is unique across all pods and namespaces unless a ConstraintTemplate has access to all other pods and namespaces. To enable this use case, we provide syncing of data into a data client.

### Replicating Data with SyncSets (Recommended)

`Feature State`: Gatekeeper version v3.15+ (alpha)

Kubernetes data can be replicated into the data client using `SyncSet` resources. Below is an example of a `SyncSet`:

```yaml
apiVersion: syncset.gatekeeper.sh/v1alpha1
kind: SyncSet
metadata:
  name: syncset-1
spec:
  gvks:
    - group: ""
      version: "v1"
      kind: "Namespace"
    - group: ""
      version: "v1"
      kind: "Pod"
```

The resources defined in the `gvks` field of a SyncSet will be eventually synced into the data client.

#### Working with SyncSet resources

* Updating a SyncSet's `gvks` field should dynamically update what objects are synced.
* Multiple `SyncSet`s may be defined and those will be reconciled by the Gatekeeper syncset-controller. Notably, the [set union](https://en.wikipedia.org/wiki/Union_(set_theory)) of all SyncSet resources' `gvks` and the [Config](sync#replicating-data-with-config) resource's `syncOnly` will be synced into the data client.
* A resource will continue to be present in the data client so long as a SyncSet or Config still specifies it under the `gvks` or `syncOnly` field.

### Replicating Data with Config

`Feature State`: Gatekeeper version v3.6+ (alpha)

> The "Config" resource must be named `config` for it to be reconciled by Gatekeeper. Gatekeeper will ignore the resource if you do not name it `config`.

Kubernetes data can also be replicated into the data client via the Config resource. Resources defined in `syncOnly` will be synced into OPA. Below is an example:

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  sync:
    syncOnly:
      - group: ""
        version: "v1"
        kind: "Namespace"
      - group: ""
        version: "v1"
        kind: "Pod"
```

You can install this config with the following command:

```sh
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/demo/basic/sync.yaml
```

#### Working with Config resources

* Updating a Config's `syncOnly` field should dynamically update what objects are synced.
* The `Config` resource is meant to be a singleton. The [set union](https://en.wikipedia.org/wiki/Union_(set_theory)) of all SyncSet resources' `gvks` and the [Config](sync#replicating-data-with-config) resource's `syncOnly` will be synced into the data client.
* A resource will continue to be present in the data client so long as a SyncSet or Config still specifies it under the `gvks` or `syncOnly` field.

### Accessing replicated data

Once data is synced, ConstraintTemplates can access the cached data under the `data.inventory` document.

The `data.inventory` document has the following format:
  * For cluster-scoped objects: `data.inventory.cluster[<groupVersion>][<kind>][<name>]`
     * Example referencing the Gatekeeper namespace: `data.inventory.cluster["v1"].Namespace["gatekeeper"]`
  * For namespace-scoped objects: `data.inventory.namespace[<namespace>][groupVersion][<kind>][<name>]`
     * Example referencing the Gatekeeper pod: `data.inventory.namespace["gatekeeper"]["v1"]["Pod"]["gatekeeper-controller-manager-d4c98b788-j7d92"]`

### Auditing From Cache

The [audit](audit.md) feature does not require replication by default. However, when the `audit-from-cache` flag is set to true, the audit informer cache will be used as the source-of-truth for audit queries; thus, an object must first be cached before it can be audited for constraint violations. Kubernetes data can be replicated into the audit cache via one of the resources above.