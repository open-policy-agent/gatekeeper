---
id: audit
title: Audit
---

The audit functionality enables periodic evaluations of replicated resources against the policies enforced in the cluster to detect pre-existing misconfigurations. Audit results are stored as violations listed in the `status` field of the failed constraint.

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: ns-must-have-gk
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
  parameters:
    labels: ["gatekeeper"]
status:
  auditTimestamp: "2019-05-11T01:46:13Z"
  enforced: true
  violations:
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: default
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: gatekeeper-system
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: kube-public
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: kube-system
```

## Configuring Audit

- Audit violations per constraint: set `--constraint-violations-limit=123` (defaults to `20`)
- Audit chunk size: set `--audit-chunk-size=500` (defaults to `0` = infinite) to limit memory consumption of the auditing `Pod`
- Audit interval: set `--audit-interval=123` (defaults to every `60` seconds). Disable audit interval by setting `--audit-interval=0`

By default, the audit will request each resource from the Kubernetes API during each cycle of the audit. To instead rely on the OPA cache, use the flag `--audit-from-cache=true`. Note that this requires replication of Kubernetes resources into OPA before they can be evaluated against the enforced policies. Refer to the [Replicating data](sync.md) section for more information.

### Audit using kinds specified in the constraints only

By default, Gatekeeper will audit all resources in the cluster. This operation can take some time depending on the number of resources.

If all of your constraints match against specific kinds (e.g. "match only pods"), then you can speed up audit runs by setting `--audit-match-kind-only=true` flag. This will only check resources of the kinds specified in all [constraints](#Constraints) defined in the cluster.

For example, defining this constraint will only audit `Pod` kind:

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sAllowedRepos
metadata:
  name: prod-repo-is-openpolicyagent
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
...
```

If any of the [constraints](#Constraints) do not specify `kinds`, it will be equivalent to not setting `--audit-match-kind-only` flag (`false` by default), and will fall back to auditing all resources in the cluster.
