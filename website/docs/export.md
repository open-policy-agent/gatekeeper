---
id: export
title: Exporting violations
---

`Feature State`: Audit export in Gatekeeper version v3.13+; admission export in v3.24+ (alpha)

> ❗ This feature is alpha, subject to change (feedback is welcome!). This feature was previously known as "Consuming violations using Pubsub".

## Description

This feature exports audit violations, and optionally admission violations, to a backend from where users can consume violations.

> To gain insights into different methods of obtaining audit violations and the respective trade-offs for each approach, please refer to [Reading Audit Results](audit.md#reading-audit-results).

## Enabling Gatekeeper to export audit violations

For disk export, provide a writable mounted volume and a consumer for the exported files.

### Setting up audit to export violations

In the audit deployment, set the `--enable-violation-export` flag to `true` to export audit violations. Additionally, use `--audit-connection` (defaults to `audit-connection`) and `--audit-channel`(defaults to `audit-channel`) flags to allow audit to export violations using desired connection onto desired channel. `--audit-connection` must be set to the name of the connection config, and `--audit-channel` must be set to name of the channel where violations should get published.

A `Connection` custom resource with `spec` that contains `driver` and `config` fields is required to establish a connection for sending violations over the channel. The following example uses the disk driver to export messages:

```yaml
apiVersion: connection.gatekeeper.sh/v1alpha1
kind: Connection
metadata:
  name: audit-connection
  namespace: gatekeeper-system
spec:
  driver: "disk"
  config:
    path: "/tmp/violations/topics"
    maxAuditResults: 3
```
  - `driver` field determines which driver should be used to establish a connection. The supported value is `disk`.
  - `config` field configures the driver. For disk, set `path` to a directory on the writable mounted volume and `maxAuditResults` to the number of audit results to retain.

#### Available drivers

- Disk: Export violations to file system.

#### Status
Upon controller ingestion, the `Connection` will reflect the state of the export connection on its `status` sub resource.

```yaml
apiVersion: connection.gatekeeper.sh/v1alpha1
kind: Connection
metadata:
  name: audit-connection
  namespace: gatekeeper-system
spec:
  driver: "disk"
  config:
    path: "/tmp/violations/topics"
    maxAuditResults: 3
status:
  byPod:
  - id: "pod-id"
    connectionUID: "connection-id"
    connectionErrors:
    - type: UpsertConnection
      message: "Error message"
    publishStatuses:
    - source: audit
      active: true
      lastAttemptTime: "2026-07-28T12:00:00Z"
      lastSuccessTime: "2026-07-28T12:00:00Z"
      errors: []
    - source: webhook
      active: false
      lastAttemptTime: "2026-07-28T12:00:05Z"
      errors:
      - type: Publish
        message: "Error message"
```

The following table describes each property in the `status.byPod` section:

| Property | Type | Description |
|----------|------|-------------|
| `id` | string | Unique identifier for the pod handling the connection |
| `connectionUID` | string | Unique identifier for the specific connection instance |
| `connectionErrors` | array | Errors from creating or updating the Connection, limited to 500 entries |
| `connectionErrors[].type` | string | Type of Connection error encountered, such as `UpsertConnection` |
| `connectionErrors[].message` | string | Human-readable description of the Connection error |
| `publishStatuses` | array | Publishing health grouped by source so one producer cannot clear another producer's state |
| `publishStatuses[].source` | string | Publisher that owns the entry: `audit` or `webhook` |
| `publishStatuses[].active` | boolean | Whether the source completed at least one publish in its latest reporting window |
| `publishStatuses[].lastAttemptTime` | timestamp | Most recent publish attempt represented by this entry |
| `publishStatuses[].lastSuccessTime` | timestamp | Most recent successful publish by this source |
| `publishStatuses[].errors` | array | Publish error classes reported by this source, limited to 500 entries |

Each publisher replaces only the entry it owns. Audit updates `source: audit`, while admission violation export from the validation webhook updates `source: webhook`; either update preserves the other entry. If a source observes more than 500 distinct error classes before a successful status update, the final entry reports that additional classes were omitted. Each stored error message is the coalesced class text before the first colon and is not otherwise length-truncated.

## Enabling Gatekeeper to export admission violations

Admission violation export is independent from audit export and is disabled by default. Set `--enable-admission-violation-export=true` on the controller-manager deployment to enable it. Enabling `--enable-violation-export` on the audit deployment does not enable admission export.

Admission export uses these routing flags:

- `--audit-connection`: Connection resource name shared by audit and admission export. Defaults to `audit-connection`.
- `--audit-channel`: Channel shared by audit and admission export. Defaults to `audit-channel`.

The equivalent Helm configuration is:

```yaml
enableAdmissionViolationExport: true
exportBackend: disk
```

Admission violation export supports the disk driver. The Helm chart rejects `enableAdmissionViolationExport: true` unless `exportBackend: disk`. The supported configuration routes audit and admission export through the same `Connection`, channel, path, and volume. Admission segment retention and other spool limits are fixed internal defaults while the feature is alpha; `maxAuditResults` continues to apply only to audit results. Additional admission-specific configuration can be added later if operational feedback requires it.

The chart mounts `audit.exportVolume` at `audit.exportVolumeMount.path` in every controller-manager pod when admission export is enabled. The reader sidecar is disabled by default; configure a production reader or explicitly enable the demonstration reader with `admission.disableExportSidecar: false`. Each pod writes its own spool, so multiple webhook replicas and separate audit/webhook deployments do not contend for one file.

```yaml
enableAdmissionViolationExport: true
exportBackend: disk
audit:
  connection: audit-connection
  channel: audit-channel
  exportConnection:
    path: /tmp/violations/topics
    maxAuditResults: 3
admission:
  disableExportSidecar: true
```

Admission disk files use newline-delimited JSON. Audit and admission files can share the same channel directory because their names are distinct: audit uses `<audit-run-id>.log`, while admission uses `admission-<timestamp>-<random>.log`. A writer creates a uniquely named, locked admission `.open` segment. Reaching the fixed byte, record, or age limit flushes and closes the segment, then atomically renames it to `.log`. The demonstration reader selects only the `admission-` prefix in admission mode, so it cannot delete audit run files.

Cleanup removes only completed, unlocked `admission-` segments and enforces fixed file count, total bytes, and TTL limits. Audit cleanup explicitly ignores that prefix. Startup recovery ignores files locked by another live writer, truncates an abandoned `.open` file to its final complete JSONL record, and publishes it as a recovered `.log`. A periodic janitor starts only when recovery finds admission files or the first admission record is written, and stops after all admission artifacts are gone. The driver also reserves filesystem headroom and rejects new records before exhausting the filesystem.

Each retention-cleanup invocation removes at most 256 ready segments. Recovery may also remove abandoned empty `.open` files and `.deleting` remnants before retention runs. New disk Connections are rejected while 32 failed cleanup states are retained. Failures from Connections that were already open can temporarily take the retry map above that threshold because descriptor-owning state cannot be safely discarded.

The default chart uses per-pod `emptyDir` volumes. Unconsumed files are lost when that pod is deleted. For durable custom storage, use a separate directory or volume per pod; do not point multiple pods at the same shared directory because Connection removal owns and cleans its configured path.

Admission export messages use `eventType: violation_admission`. They include the evaluation timestamp, admission `operation` (`CREATE`, `UPDATE`, `DELETE`, or `CONNECT`), API resource and subresource, dry-run state, and requesting user's name, UID, and groups. They can describe denied requests that were never persisted in Kubernetes and include policy-provided details, resource labels, and constraint annotations. Treat the destination as security-sensitive; identity fields and policy or resource data may contain user-controlled or sensitive data.

### Default queue and disk limits

Admission export limits are fixed while the feature is alpha and apply independently to each controller-manager pod:

| Layer | Limit | Default |
|---|---|---:|
| In-memory queue | Messages being constructed or waiting to publish | 1,024 |
| In-memory queue | Total encoded bytes waiting to publish | 16 MiB |
| Publisher | Maximum records in one ready batch | 64 |
| Record | Complete encoded JSON record | 64 KiB |
| Disk segment | Maximum bytes | 1 MiB |
| Disk segment | Maximum records | 1,000 |
| Disk segment | Maximum age before rotation | 1 minute |
| Disk spool | Completed segments | 20 |
| Disk spool | Total bytes, including the open segment and pending record | 20 MiB |
| Disk spool | Maximum completed-segment age | 24 hours |
| Filesystem | Free-space reserve | 16 MiB |

The 64 KiB record limit applies to the entire JSON object after encoding, not only to the violation message. JSON field names and escaping, policy-provided details, constraint annotations, resource labels, and request identity all count toward the limit. KiB measures bytes, so the number of characters that fit varies with UTF-8 and JSON escaping. The limit leaves room for normal policy and request metadata while preventing one user-influenced result from consuming a disproportionate share of the queue and disk spool. The `kubectl.kubernetes.io/last-applied-configuration` constraint annotation is omitted, but other annotations still count. An oversized record is dropped before enqueue and reported with drop reason `message_too_large`.

Segment rotation occurs when any byte, record-count, or age limit is reached. The spool limits bound unconsumed backlog; they do not guarantee that records remain available for 24 hours. Cleanup removes the oldest completed segments until the count, byte, and age limits all hold, so count or byte pressure can remove a segment long before its maximum age. When one-minute age rotation is the limiting factor, 20 completed segments provide roughly 20 minutes of backlog; higher volume can rotate segments sooner. A reader must therefore drain segments fast enough for the workload.

The demonstration admission reader logs each record and deletes its completed segment after a successful read. It demonstrates consumption and is not a retention archive. Retain exported records in the downstream system when longer history is required.

### Delivery behavior

Admission export is best-effort. Backend publishing does not delay or change the admission response:

- Each controller-manager pod has an asynchronous queue limited to 1,024 messages, including records being constructed, and 16 MiB of encoded data.
- A complete encoded JSON record is limited to 64 KiB.
- The webhook reserves count capacity before constructing a record. The publisher drains up to 64 records that are already queued without waiting to fill a batch.
- Messages are dropped when encoding fails or an enqueue limit is reached. A failed publish is not retried.
- During graceful termination, messages still in the queue are drained for up to five seconds. Messages still queued after that deadline are dropped and reported with the `shutdown` reason.
- A batch already removed from the queue when termination begins is not requeued. Gatekeeper stops waiting when its publish context is canceled; a backend call that ignores cancellation may finish later, but its result is not used and the process may exit first.
- `SIGKILL`, OOM termination, and node failure do not run the drain. Queued and in-flight records are then lost. Complete disk records already written and synced can be recovered only if the volume survives; the default `emptyDir` survives a container restart but not pod deletion or replacement.
- Repeated drop and publish-error logs are limited to one per minute for each class; use the admission export metrics to detect sustained loss.
- Webhook publish status is reported outside the queue publisher. Errors and health transitions are checked every 10 seconds, while unchanged healthy traffic is coalesced to a one-minute heartbeat. Audit updates only the `audit` entry at the end of each run.
- A source with no new publish attempts leaves its previous entry unchanged. A later successful reporting window clears only that source's errors; use `lastAttemptTime` and `lastSuccessTime` to evaluate freshness.

### Quick start with exporting admission violations to disk

This quick start enables the demonstration admission reader. It logs completed admission records and deletes each segment after reading it, so use it only to verify the feature.

The selected Connection name must be unused or already owned by this Helm release.

1. Install or upgrade Gatekeeper with admission export, the disk backend, and the demonstration reader enabled:

    ```shell
    helm upgrade --install gatekeeper gatekeeper/gatekeeper \
      --namespace gatekeeper-system \
      --create-namespace \
      --set enableAdmissionViolationExport=true \
      --set exportBackend=disk \
      --set admission.disableExportSidecar=false \
      --set audit.connection=audit-connection \
      --set audit.channel=audit-channel

    kubectl rollout status deployment/gatekeeper-controller-manager \
      --namespace gatekeeper-system \
      --timeout=2m
    ```

2. Install a test policy that requires the `test` label on Pods in the `nginx` namespace:

    ```shell
    export GATEKEEPER_EXPORT_EXAMPLES=https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/test/export

    kubectl create namespace nginx --dry-run=client --output yaml | kubectl apply -f -
    kubectl apply -f "${GATEKEEPER_EXPORT_EXAMPLES}/k8srequiredlabels_ct.yaml"
    until kubectl get crd k8srequiredlabels.constraints.gatekeeper.sh >/dev/null 2>&1; do sleep 1; done
    kubectl apply -f "${GATEKEEPER_EXPORT_EXAMPLES}/pod_must_have_test.yaml"
    until kubectl get k8srequiredlabels pod-must-have-test \
      -o jsonpath='{.status.byPod[*].operations[*]}' 2>/dev/null | grep -qw webhook; do sleep 1; done
    ```

3. Submit a Pod that violates the policy. The command is expected to fail with a denial:

    ```shell
    kubectl apply -f - <<'EOF'
    apiVersion: v1
    kind: Pod
    metadata:
      name: denied-export-pod
      namespace: nginx
      labels:
        app: denied-export
    spec:
      containers:
      - name: nginx
        image: nginx:latest
    EOF
    ```

4. Confirm that a controller-manager reader observed the admission record:

    ```shell
    kubectl logs --namespace gatekeeper-system \
      --selector control-plane=controller-manager \
      --container admission-reader \
      --since=5m | grep '"eventType":"violation_admission"' | grep '"resourceName":"denied-export-pod"'
    ```

    The webhook publisher also reports health under `source: webhook`:

    ```shell
    kubectl get connection audit-connection \
      --namespace gatekeeper-system \
      --output yaml
    ```

### Quick start with exporting violations on node storage using Disk driver via emptyDir

This quick start uses the demonstration reader and a per-pod `emptyDir`; unread records are lost when a pod is deleted or replaced. The selected Connection name must be unused or already owned by this Helm release.

#### Configure Gatekeeper with Export enabled to Disk

1. Deploy Gatekeeper with disk export configurations.

    Below are the default configurations that enable disk export and add a sidecar container to the Gatekeeper audit pod:

    ```yaml
    audit: 
      exportVolume: 
        name: tmp-violations 
        emptyDir: {} 
      exportVolumeMount: 
        path: /tmp/violations 
      exportSidecar: 
        name: reader
        image: ghcr.io/open-policy-agent/fake-reader:latest
        imagePullPolicy: Always 
        securityContext: 
          allowPrivilegeEscalation: false 
          capabilities: 
            drop: 
            - ALL 
          readOnlyRootFilesystem: true 
          runAsGroup: 999 
          runAsNonRoot: true 
          runAsUser: 1000 
          seccompProfile: 
            type: RuntimeDefault 
        volumeMounts: 
        - mountPath: /tmp/violations 
          name: tmp-violations
    ```

    :::warning
    The reader sidecar image `ghcr.io/open-policy-agent/fake-reader` is published as part of each Gatekeeper release and is intended for demonstration and quickstart purposes only. It is not recommended for production environments. For production use, it is advised to create and configure a custom sidecar image tailored to your specific requirements.
    :::

    ```shell
    helm upgrade --install gatekeeper gatekeeper/gatekeeper --namespace gatekeeper-system \
    --create-namespace \
    --set enableViolationExport=true \
    --set audit.connection=audit-connection \
    --set audit.channel=audit-channel \
    --set audit.exportConnection.path=/tmp/violations/topics \
    --set audit.exportConnection.maxAuditResults=3 \
    --set exportBackend=disk
    ```
    
    As part of the command above, the `Connection` resource is installed with the following values and defaults:

    ```yaml
    apiVersion: connection.gatekeeper.sh/v1alpha1
    kind: Connection
    metadata:
      name: "audit-connection"
      namespace: "gatekeeper-system"
    spec:
      driver: "disk"
      config:
        path: "/tmp/violations/topics"
        maxAuditResults: 3
    ```

    | Property        | Description                                                                                                                                                            | Default                  |
    |:----------------|:---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |:-------------------------|
    | path            | (alpha) Path for audit pod manager container to export violations and sidecar container to read from. Must be a child of volume mount path so the parent is writable.  | "/tmp/violations/topics" |
    | maxAuditResults | (alpha) Maximum number of audit results that can be stored in the export path.                                                      | 3                 |
    | closedConnectionTTL | (alpha) Optional duration string controlling how long a failed closed Connection remains eligible for cleanup retries. Accepted values are `"1m"` through `"10m"`. The chart leaves it unset and the driver defaults to `"10m"`. | `"10m"` |

    **Note**: After the audit pod starts, verify that it contains two running containers.

    ```shell
    kubectl get pod -n gatekeeper-system 
    NAME                                             READY   STATUS    RESTARTS        AGE
    gatekeeper-audit-6865f5f56d-vclxw                2/2     Running   0               12s
    ```

    :::tip
    The command above deploys the audit pod with a default sidecar reader and volume. To customize the sidecar reader or volume according to your requirements, you can set the following variables in your values.yaml file:

    ```yaml
    audit: 
      exportVolume: 
        <your-volume>
      exportVolumeMount: 
        path: <volume-mount-path>
      exportSidecar: 
        <your-side-car>
    ```
    :::

2. Create the constraint templates and constraints, and make sure audit ran by checking constraints. If constraint status is updated with information such as `auditTimeStamp` or `totalViolations`, then audit has ran at least once. Additionally, populated `TOTAL-VIOLATIONS` field for all constraints while listing constraints also indicates that audit has ran at least once.

    ```log
    kubectl get constraint
    NAME                 ENFORCEMENT-ACTION   TOTAL-VIOLATIONS
    pod-must-have-test                        0
    ```

3. Finally, check the sidecar reader logs to see the violations written.

    ```log
    kubectl logs -l gatekeeper.sh/operation=audit -c reader -n gatekeeper-system
    2025/03/05 00:37:16 {"id":"2025-03-05T00:37:13Z","details":{"missing_labels":["test"]},"eventType":"violation_audited","group":"constraints.gatekeeper.sh","version":"v1beta1","kind":"K8sRequiredLabels","name":"pod-must-have-test","message":"you must provide labels: {\"test\"}","enforcementAction":"deny","resourceAPIVersion":"v1","resourceKind":"Pod","resourceNamespace":"nginx","resourceName":"nginx-deployment-2-79479fc6db-7qbnm","resourceLabels":{"app":"nginx-ingress","app.kubernetes.io/component":"controller","pod-template-hash":"79479fc6db"}}
    ```

### Violations

The audit pod exports violations in following format:

```json
{
  "id": "2023-07-18T21:21:52Z",
  "details": {
    "missing_labels": [
      "test"
    ]
  },
  "eventType": "violation_audited",
  "group": "constraints.gatekeeper.sh",
  "version": "v1beta1",
  "kind": "K8sRequiredLabels",
  "name": "pod-must-have-test",
  "message": "you must provide labels: {\"test\"}",
  "enforcementAction": "deny",
  "resourceAPIVersion": "v1",
  "resourceKind": "Pod",
  "resourceNamespace": "nginx",
  "resourceName": "nginx-deployment-cd55c47f5-2b84x",
  "resourceLabels": {
    "app": "nginx",
    "pod-template-hash": "cd55c47f5"
  }
}
```
