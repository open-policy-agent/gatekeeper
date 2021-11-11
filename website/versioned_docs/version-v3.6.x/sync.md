---
id: sync
title: Replicating Data
---

> The "Config" resource has to be named "config" for it to be reconciled by Gatekeeper. Gatekeeper will ignore the resource if you do not name it "config".

Some constraints are impossible to write without access to more state than just the object under test. For example, it is impossible to know if an ingress's hostname is unique among all ingresses unless a rule has access to all other ingresses. To make such rules possible, we enable syncing of data into OPA.

The [audit](audit.md) feature does not require replication by default. However, when the ``audit-from-cache`` flag is set to true, the OPA cache will be used as the source-of-truth for audit queries; thus, an object must first be cached before it can be audited for constraint violations.

Kubernetes data can be replicated into OPA via the sync config resource. Currently resources defined in `syncOnly` will be synced into OPA. Updating `syncOnly` should dynamically update what objects are synced. Below is an example:

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

Once data is synced into OPA, rules can access the cached data under the `data.inventory` document.

The `data.inventory` document has the following format:

  * For cluster-scoped objects: `data.inventory.cluster[<groupVersion>][<kind>][<name>]`
     * Example referencing the Gatekeeper namespace: `data.inventory.cluster["v1"].Namespace["gatekeeper"]`
  * For namespace-scoped objects: `data.inventory.namespace[<namespace>][groupVersion][<kind>][<name>]`
     * Example referencing the Gatekeeper pod: `data.inventory.namespace["gatekeeper"]["v1"]["Pod"]["gatekeeper-controller-manager-d4c98b788-j7d92"]`