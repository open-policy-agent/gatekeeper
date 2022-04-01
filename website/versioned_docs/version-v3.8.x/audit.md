---
id: audit
title: Audit
---

Audit performs periodic evaluations of existing resources against constraints, detecting pre-existing misconfigurations.

## Reading Audit Results

There are three ways to gather audit results, depending on the level of detail needed.

### Prometheus Metrics

Prometheus metrics provide an aggregated look at the number of audit violations:

* `gatekeeper_audit_last_run_time` provides the timestamp of the most recently completed audit run
* `gatekeeper_violations` provides the total number of audited violations for the last audit run, broken down by violation severity

### Constraint Status

Violations of constraints are listed in the `status` field of the corresponding constraint.
Note that only violations from the most recent audit run are reported. Also note that there
is a maximum number of individual violations that will be reported on the constraint
itself. If the number of current violations is greater than this cap, the excess violations
will not be reported (though they will still be included in the `totalViolations` count).
This is because Kubernetes has a cap on how large individual API objects can grow, which makes
unbounded growth a bad idea. This limit can be configured via the `--constraint-violations-limit` flag.

Here is an example of a constraint with violations:

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

### Audit Logs

#### Violations

The audit pod emits JSON-formatted audit logs to stdout. The following is an example audit event:

```json
{
  "level": "info",
  "ts": 1632889070.3075402,
  "logger": "controller",
  "msg": "container <kube-scheduler> has no resource limits",
  "process": "audit",
  "audit_id": "2021-09-29T04:17:47Z",
  "event_type": "violation_audited",
  "constraint_group": "constraints.gatekeeper.sh",
  "constraint_api_version": "v1beta1",
  "constraint_kind": "K8sContainerLimits",
  "constraint_name": "container-must-have-limits",
  "constraint_namespace": "",
  "constraint_action": "deny",
  "resource_group": "",
  "resource_api_version": "v1",
  "resource_kind": "Pod",
  "resource_namespace": "kube-system",
  "resource_name": "kube-scheduler-kind-control-plane"
}
```

In addition to information on the violated constraint, violating resource, and violation message, the
audit log entries also contain:

* An `audit_id` field that uniquely identifies a given audit run. This allows indexing of historical audits
* An `event_type` field with a value of `violation_audited` to make it easy to programatically identify audit violations

#### Other Event Types

In addition to violations, these other audit events may be useful (all uniquely identified via the `event_type` field):

* `audit_started` marks the beginning of a new audit run
* `constraint_audited` marks when a constraint is done being audited for a given run, along with the number of violations found
* `audit_finished` marks the end of the current audit run

All of these events (including `violation_audited`) are marked with the same `audit_id` for a given audit run.

## Configuring Audit

- Audit violations per constraint: set `--constraint-violations-limit=123` (defaults to `20`)
- Audit chunk size: set `--audit-chunk-size=400` (defaults to `500`, `0` = infinite) Lower chunk size can reduce memory consumption of the auditing `Pod` but can increase the number requests to the Kubernetes API server.
- Audit interval: set `--audit-interval=123` (defaults to every `60` seconds). Disable audit interval by setting `--audit-interval=0`
- Audit api server cache write to disk (Gatekeeper v3.7.0+): Starting from v3.7.0, by default, audit writes api server cache to the disk attached to the node. This reduces the memory consumption of the audit `pod`. If there are concerns with high IOPS, then switch audit to write cache to a tmpfs ramdisk instead. NOTE: write to ramdisk will increase memory footprint of the audit `pod`.  
  - helm install `--set audit.writeToRAMDisk=true` 
  - if not using helm, modify the deployment manifest to mount a ramdisk
    ```yaml
    - emptyDir:
        medium: Memory
    ```

By default, audit will request each resource from the Kubernetes API during each audit cycle. To rely on the OPA cache instead, use the flag `--audit-from-cache=true`. Note that this requires replication of Kubernetes resources into OPA before they can be evaluated against the enforced policies. Refer to the [Replicating data](sync.md) section for more information.

### Audit using kinds specified in the constraints only

By default, Gatekeeper will audit all resources in the cluster. This operation can take some time depending on the number of resources.

If all of your constraints match against specific kinds (e.g. "match only pods"), then you can speed up audit runs by setting `--audit-match-kind-only=true` flag. This will only check resources of the kinds specified in all [constraints](howto.md#constraints) defined in the cluster.

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

If any of the [constraints](howto.md#constraints) do not specify `kinds`, it will be equivalent to not setting `--audit-match-kind-only` flag (`false` by default), and will fall back to auditing all resources in the cluster.
