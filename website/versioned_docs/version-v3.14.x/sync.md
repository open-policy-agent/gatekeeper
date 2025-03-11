---
id: sync
title: Replicating Data
---

`Feature State`: The `Config` resource is currently alpha.

> The "Config" resource must be named `config` for it to be reconciled by Gatekeeper. Gatekeeper will ignore the resource if you do not name it `config`.

Some constraints are impossible to write without access to more state than just the object under test. For example, it is impossible to know if an ingress's hostname is unique among all ingresses unless a rule has access to all other ingresses. To make such rules possible, we enable syncing of data into OPA.

The [audit](audit.md) feature does not require replication by default. However, when the ``audit-from-cache`` flag is set to true, the audit informer cache will be used as the source-of-truth for audit queries; thus, an object must first be cached before it can be audited for constraint violations.

Kubernetes data can be replicated into the audit cache via the sync config resource. Currently resources defined in `syncOnly` will be synced into OPA. Updating `syncOnly` should dynamically update what objects are synced. Below is an example:

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
Once data is synced, ConstraintTemplates can access the cached data under the `data.inventory` document. ConstraintTemplates must specify the Group/Version/Kinds that they will need in a `metadata.gatekeeper.sh/requires-sync-data` annotation. This is specified in a format like this:
```
[
  [ // Requirement 1
    { // Clause 1
      "groups": ["group1", group2"]
      "versions": ["version1", "version2", "version3"]
      "kinds": ["kind1", "kind2"]
    },
    { // Clause 2
      "groups": ["group3", group4"]
      "versions": ["version3", "version4"]
      "kinds": ["kind3", "kind4"]
    }
  ],
  [ // Requirement 2
    { // Clause 1
      "groups": ["group5"]
      "versions": ["version5"]
      "kinds": ["kind5"]
    }
  ]
]
```
Each clause within the requirement is treated as a logical OR. So "group1/version1/kind1" OR "group2/version2/kind2" OR "group2/version1/kind1", etc.
So, for example, if a Constraint Template required to sync PodDisruptionBudgets, Deployments, and StatefulSets, the annotation would be as follows:
```
metadata.gatekeeper.sh/requires-sync-data: |
      "[
        [
          {
            "groups": ["policy"],
            "versions": ["v1"],
            "kinds": ["PodDisruptionBudget"]
          }
        ],
        [
          {
            "groups": ["apps"],
            "versions": ["v1"],
            "kinds": ["Deployment"]
          }
        ],
        [
          {
            "groups": ["apps"],
            "versions": ["v1"],
            "kinds": ["StatefulSet"]
          }
        ]
      ]
      "
```

The `data.inventory` document has the following format:

  * For cluster-scoped objects: `data.inventory.cluster[<groupVersion>][<kind>][<name>]`
     * Example referencing the Gatekeeper namespace: `data.inventory.cluster["v1"].Namespace["gatekeeper"]`
  * For namespace-scoped objects: `data.inventory.namespace[<namespace>][groupVersion][<kind>][<name>]`
     * Example referencing the Gatekeeper pod: `data.inventory.namespace["gatekeeper"]["v1"]["Pod"]["gatekeeper-controller-manager-d4c98b788-j7d92"]`
