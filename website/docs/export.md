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

Install prerequisites such as a pubsub tool, a message broker etc.

### Setting up audit to export violations

In the audit deployment, set the `--enable-violation-export` flag to `true` to export audit violations. Additionally, use `--audit-connection` (defaults to `audit-connection`) and `--audit-channel`(defaults to `audit-channel`) flags to allow audit to export violations using desired connection onto desired channel. `--audit-connection` must be set to the name of the connection config, and `--audit-channel` must be set to name of the channel where violations should get published.

A `Connection` custom resource with `spec` that contains `driver` and `config` fields are required to establish connection for sending violations over the channel. Following is an example to establish a connection that uses Dapr to export messages:

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
- `driver` field determines which tool/driver should be used to establish a connection. Valid values are: `dapr`, `disk`
- `config` field is an object that configures how the connection is made. E.g. which queue messages should be sent to.

#### Available drivers

- Dapr: Export violations using pubsub model provided with [Dapr](https://dapr.io/)
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
  driver: "dapr"
  config:
    component: "pubsub"
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

Admission violation export currently supports only the disk driver. The Helm chart rejects `enableAdmissionViolationExport: true` unless `exportBackend: disk`; the Dapr driver is not supported for admission export. The supported configuration routes audit and admission export through the same `Connection`, channel, path, and volume. Admission segment retention and other spool limits are fixed internal defaults while the feature is alpha; `maxAuditResults` continues to apply only to audit results. Additional admission-specific configuration can be added later if operational feedback requires it.

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

Cleanup removes only completed, unlocked `admission-` segments and enforces fixed file count, total bytes, and TTL limits. Audit cleanup explicitly ignores that prefix. A periodic janitor applies TTL while traffic is idle. Startup recovery ignores files locked by another live writer, truncates an abandoned `.open` file to its final complete JSONL record, and publishes it as a recovered `.log`. The driver also reserves filesystem headroom and rejects new records before exhausting the filesystem.

Each retention-cleanup invocation removes at most 256 ready segments. Recovery may also remove abandoned empty `.open` files and `.deleting` remnants before retention runs. New disk Connections are rejected while 32 failed cleanup states are retained. Failures from Connections that were already open can temporarily take the retry map above that threshold because descriptor-owning state cannot be safely discarded.

The default chart uses per-pod `emptyDir` volumes. Unconsumed files are lost when that pod is deleted. For durable custom storage, use a separate directory or volume per pod; do not point multiple pods at the same shared directory because Connection removal owns and cleans its configured path.

Admission export messages use `eventType: violation_admission`. They include the evaluation timestamp, admission `operation` (`CREATE`, `UPDATE`, `DELETE`, or `CONNECT`), API resource and subresource, dry-run state, and requesting user's name, UID, and groups. They can describe denied requests that were never persisted in Kubernetes and include policy-provided details, resource labels, and constraint annotations. Treat the destination as security-sensitive; identity fields and policy or resource data may contain user-controlled or sensitive data.

### Default queue and disk limits

Admission export limits are fixed while the feature is alpha and apply independently to each controller-manager pod:

| Layer | Limit | Default |
|---|---|---:|
| In-memory queue | Messages waiting to publish | 1,024 |
| In-memory queue | Total encoded bytes waiting to publish | 16 MiB |
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

- Each controller-manager pod has an asynchronous queue limited to 1,024 messages and 16 MiB of encoded data.
- A complete encoded JSON record is limited to 64 KiB.
- Messages are dropped when encoding fails or an enqueue limit is reached. A failed publish is not retried.
- Queued messages are drained for up to five seconds during shutdown. Messages still queued after that deadline are dropped and reported with the `shutdown` reason.
- Repeated drop and publish-error logs are limited to one per minute for each class; use the admission export metrics to detect sustained loss.
- Webhook publish success and errors are written to the `webhook` entry in `ConnectionPodStatus.status.publishStatuses` every 10 seconds instead of once per message. Audit updates only the `audit` entry at the end of each run.
- A source with no new publish attempts leaves its previous entry unchanged. A later successful reporting window clears only that source's errors; use `lastAttemptTime` and `lastSuccessTime` to evaluate freshness.

### Quick start with exporting audit violations using Dapr and Redis

#### Prerequisites for Dapr

1. Install Dapr

   To install Dapr with specific requirements and configuration, please refer to [Dapr docs](https://docs.dapr.io/operations/hosting/kubernetes/kubernetes-deploy/).

  :::important
    - Make sure to set `SIDECAR_DROP_ALL_CAPABILITIES` environment variable on `dapr-sidecar` injector pod to `true` to avoid getting `PodSecurity violation` errors for the injected sidecar container as Gatekeeper by default requires workloads to run with [restricted](https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted) policy. If using helm charts to install Dapr, you can use `--set dapr_sidecar_injector.sidecarDropALLCapabilities=true`.
    - Additionally, [configure appropriate seccompProfile for sidecar containers](https://docs.dapr.io/operations/hosting/kubernetes/kubernetes-production/#configure-seccompprofile-for-sidecar-containers) injected by Dapr to avoid getting `PodSecurity violation` errors. We are setting required Dapr annotation for audit pod while deploying Gatekeeper later in this quick start to avoid getting `PodSecurity violation` error.
  :::

    > Dapr is installed with mtls enabled by default, for more details on the same please refer to [Dapr security](https://docs.dapr.io/operations/security/mtls/#setting-up-mtls-with-the-configuration-resource).

2. Install Redis

    Please refer to [this](https://docs.dapr.io/getting-started/tutorials/configure-state-pubsub/#step-1-create-a-redis-store) guide to install Redis.

    > Redis is used for example purposes only. Dapr supports [many different state store options](https://docs.dapr.io/reference/components-reference/supported-state-stores/). To install Redis with TLS, please refer to [this](https://docs.bitnami.com/kubernetes/infrastructure/redis-cluster/administration/enable-tls/) doc.

#### Configure a sample subscriber to receive violations

1. Create `fake-subscriber` namespace and redis secret

    ```shell
    kubectl create ns fake-subscriber
    # creating redis secret in subscriber namespace to allow Dapr sidecar to connect to redis instance
    kubectl get secret redis --namespace=default -o yaml | sed 's/namespace: .*/namespace: fake-subscriber/' | kubectl apply -f -
    ```

2. Create Dapr pubsub component

    ```shell
    kubectl apply -f - <<EOF
    apiVersion: dapr.io/v1alpha1
    kind: Component
    metadata:
      name: pubsub
      namespace: fake-subscriber
    spec:
      type: pubsub.redis
      version: v1
      metadata:
      - name: redisHost
        value: redis-master.default.svc.cluster.local:6379
      - name: redisPassword
        secretKeyRef: 
          name: redis
          key: redis-password
    EOF
    ```

    > Please use [this guide](https://docs.dapr.io/reference/components-reference/supported-state-stores/setup-redis/) to properly configure Redis pubsub component for Dapr.

3. Deploy subscriber application

    ```yaml
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: sub
      namespace: fake-subscriber
      labels:
        app: sub
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: sub
      template:
        metadata:
          labels:
            app: sub
          annotations:
            dapr.io/enabled: "true"
            dapr.io/app-id: "subscriber"
            dapr.io/enable-api-logging: "true"
            dapr.io/app-port: "6002"
        spec:
          containers:
          - name: go-sub
            image: ghcr.io/open-policy-agent/fake-subscriber:latest
            imagePullPolicy: Always
    ```

    :::important
    The `fake-subscriber` image is published as part of each Gatekeeper release at `ghcr.io/open-policy-agent/fake-subscriber`. To build a custom version locally, use the Dockerfile at [gatekeeper/test/export/fake-subscriber](https://github.com/open-policy-agent/gatekeeper/tree/master/test/export/fake-subscriber).
    :::

#### Configure Gatekeeper with Export enabled with Dapr

1. Create Gatekeeper namespace, and create Dapr pubsub component and Redis secret in Gatekeeper's namespace (`gatekeeper-system` by default). Please make sure to update `gatekeeper-system` namespace for the next steps if your cluster's Gatekeeper namespace is different.

    ```shell
    kubectl create namespace gatekeeper-system
    kubectl get secret redis --namespace=default -o yaml | sed 's/namespace: .*/namespace: gatekeeper-system/' | kubectl apply -f -
    kubectl apply -f - <<EOF
    apiVersion: dapr.io/v1alpha1
    kind: Component
    metadata:
      name: pubsub
      namespace: gatekeeper-system
    spec:
      type: pubsub.redis
      version: v1
      metadata:
      - name: redisHost
        value: redis-master.default.svc.cluster.local:6379
      - name: redisPassword
        secretKeyRef:
          name: redis
          key: redis-password
    EOF
    ```

2. To upgrade or install Gatekeeper with `--enable-violation-export` set to `true`, `--audit-connection` set to `audit-connection`, `--audit-channel` set to `audit-channel` on audit pod.

    ```shell
    # auditPodAnnotations is used to add annotations required by Dapr to inject sidecar to audit pod
    echo 'auditPodAnnotations: {dapr.io/enabled: "true", dapr.io/app-id: "audit", dapr.io/metrics-port: "9999", dapr.io/sidecar-seccomp-profile-type: "RuntimeDefault"}' > /tmp/annotations.yaml
    helm upgrade --install gatekeeper gatekeeper/gatekeeper --namespace gatekeeper-system \
    --set enableViolationExport=true \
    --set audit.connection=audit-connection \
    --set audit.channel=audit-channel \
    --values /tmp/annotations.yaml
    ```

    **Note:** Verify that after the audit pod is running there is a Dapr sidecar injected and running along side `manager` container.

3. Create connection config to establish a connection.

    ```shell
    kubectl apply -f - <<EOF
    apiVersion: connection.gatekeeper.sh/v1alpha1
    kind: Connection
    metadata:
      name: audit-connection
      namespace: gatekeeper-system
    spec:
      driver: "dapr"
      config:
        component: "pubsub"
    EOF
    ```

    **Note:** Name of the `Connection` custom resource must match the value of `--audit-connection` for it to be used by audit to export violation. At the moment, only one connection can exist for audit.

4. Create the constraint templates and constraints, and make sure audit ran by checking constraints. If constraint status is updated with information such as `auditTimeStamp` or `totalViolations`, then audit has ran at least once. Additionally, populated `TOTAL-VIOLATIONS` field for all constraints while listing constraints also indicates that audit has ran at least once.

    ```log
    kubectl get constraint
    NAME                 ENFORCEMENT-ACTION   TOTAL-VIOLATIONS
    pod-must-have-test                        0
    ```

5. Finally, check the subscriber logs to see the violations received.

    ```log
    kubectl logs -l app=sub -c go-sub -n fake-subscriber 
    2023/07/18 20:16:41 Listening...
    2023/07/18 20:37:20 main.ExportMsg{ID:"2023-07-18T20:37:19Z", Details:map[string]interface {}{"missing_labels":[]interface {}{"test"}}, EventType:"violation_audited", Group:"constraints.gatekeeper.sh", Version:"v1beta1", Kind:"K8sRequiredLabels", Name:"pod-must-have-test", Namespace:"", Message:"you must provide labels: {\"test\"}", EnforcementAction:"deny", ConstraintAnnotations:map[string]string(nil), ResourceGroup:"", ResourceAPIVersion:"v1", ResourceKind:"Pod", ResourceNamespace:"nginx", ResourceName:"nginx-deployment-58899467f5-j85bs", ResourceLabels:map[string]string{"app":"nginx", "owner":"admin", "pod-template-hash":"58899467f5"}}
    ```

### Quick start with exporting violations on node storage using Disk driver via emptyDir

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
    --set enableViolationExport=true \
    --set audit.connection=audit-connection \
    --set audit.channel=audit-channel \
    --set audit.exportConnection.path=tmp/violations/topics \
    --set audit.exportConnection.maxAuditResults=3 \
    --set exportBackend=disk \
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
        closedConnectionTTL: 600
    ```

    | Property        | Description                                                                                                                                                            | Default                  |
    |:----------------|:---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |:-------------------------|
    | path            | (alpha) Path for audit pod manager container to export violations and sidecar container to read from. Must be a child of volume mount path so the parent is writable.  | "/tmp/violations/topics" |
    | maxAuditResults | (alpha) Maximum number of audit results that can be stored in the export path.                                                      | 3                 |
    | closedConnectionTTL | (alpha) TTL in seconds for connection to be in the retry queue after it is closed/deleted in case of failure.                                                   | 600                 |

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
    kubectl logs -l gatekeeper.sh/operation=audit -c go-sub -n gatekeeper-system 
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
