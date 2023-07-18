---
id: pubsub
title: Consuming violations using Pubsub
---

`Feature State`: Gatekeeper version v3.13+ (alpha)

> ❗ This feature is alpha, subject to change (feedback is welcome!).

## Motivation

Prior to this feature, there were two ways to get audit violations. One is to look at constraints status and the other is to look at audit pod logs to get the logged audit violations. Both of these approaches have limitations as described below.

Limitations of getting audit violations from constraint status:

- To reduce in-memory consumption of Gatekeeper audit pod and to avoid hitting [default etcd limit](https://etcd.io/docs/v3.5/dev-guide/limit/#request-size-limit) of 1.5MB per resource, gatekeeper recommends configuring a [limit up-to 500 violations](https://open-policy-agent.github.io/gatekeeper/website/docs/audit/#configuring-audit)(by default 20) on constraint template. Because of these limitations, users might not get all the violations from a Constraint resource.

Limitations of getting audit violations from audit logs:

- It could be difficult to parse audit pod logs to look for violation messages, as violation logs would be mixed together with other log statements. Additionally, when there are huge number of violations, it’s possible to miss part of the log as the pod logs get rotated.

This feature uses publish and subscribe (pubsub) model that allows Gatekeeper to export audit violations over a broker that can be consumed by a subscriber independently. Therefore, it allows users to get all the audit violations.

## Enabling Gatekeeper to export audit violations

Install prerequisites such as a pubsub tool, a message broker etc.

### Setting up audit with pubsub enabled

In the audit deployment, set the `--enable-pub-sub` flag to `true` to publish audit violations. Additionally, `--audit-connection` and `--audit-channel` flags must be set to allow audit to publish violations. `--audit-connection` must be set to the name of the connection config, and `--audit-channel` must be set to name of the channel where violations should get published.

Create a connection configMap that supplies appropriate set of configurations for a connection to get established. For instance, to establish a connection that uses Dapr to publish messages this configMap is appropriate:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: audit
  namespace: gatekeeper-system
data:
  provider: "dapr"
  config: |
    {
      "component": "pubsub"
    }
```

- `provider` field in `configMap.data` determined which tool/driver should be used to establish a connection. Valid values are: `dapr`
- `config` field in `configMap.data` is a json data that allows users to pass appropriate information to establish connection to use respective provider.

#### Available Pubsub drivers
Dapr: https://dapr.io/

### Violations

The audit pod publishes violations in following format:

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

### Quick start with publishing violations using Dapr and Redis

> Redis is used for example purposes only. Dapr supports [many different state store options](https://docs.dapr.io/reference/components-reference/supported-state-stores/).

#### Prerequisites

1. Install Dapr

    ```shell
    helm repo add dapr https://dapr.github.io/helm-charts/
    DAPR_VERSION=1.10
    helm upgrade --install dapr dapr/dapr --version=${DAPR_VERSION} --namespace dapr-system --create-namespace --wait --debug
    ```

    To install dapr with specific requirements and configuration, please refer to [dapr docs](https://docs.dapr.io/getting-started/).
    > Dapr is installed with mtls enabled by default, for more details on the same plaase refer to [dapr security](https://docs.dapr.io/operations/security/mtls/#setting-up-mtls-with-the-configuration-resource).

2. Install Redis

    ```shell
    helm repo add bitnami https://charts.bitnami.com/bitnami
    REDIS_IMAGE_TAG=7.0-debian-11
    helm upgrade --install redis bitnami/redis --namespace default --set image.tag=${REDIS_IMAGE_TAG} --wait --debug
    ```

    > To install Redis with TLS, please refer to [this](https://docs.bitnami.com/kubernetes/infrastructure/redis-cluster/administration/enable-tls/) doc.

#### Configure a sample subscriber to receive violations

1. Create `fake-subscriber` namespace and redis secret

```shell
kubectl create ns fake-subscriber
kubectl get secret redis --namespace=default -o yaml | sed 's/namespace: .*/namespace: fake-subscriber/' | kubectl apply -f - # creating redis secret in subscriber namespace to allow dapr sidecar to connect to redis instance
```

2. Create Dapr pubsub component
```shell
kubectl apply -f <<EOF
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
        image: fake-subscriber:latest
        imagePullPolicy: Never
```
**Note:** Dockerfile to build image for fake-subscriber is under [gatekeeper/test/fake-subscriber](https://github.com/open-policy-agent/gatekeeper/tree/master/test/pubsub/fake-subscriber). You can find make rule to build and deploy subscriber in [Makefile](https://github.com/open-policy-agent/gatekeeper/blob/master/Makefile) under name `e2e-subscriber-build-load-image` and `e2e-subscriber-deploy`.

#### Configure Gatekeeper with Pubsub enabled

1. Create Dapr pubsub component and Redis secret in Gatekeeper's namespace (`gatekeeper-system` by default). Please make sure to update `gatekeeper-system` namespace for the next steps if your cluster's Gatekeeper namespace is different.

```shell
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

2. Install Gatekeeper with `--enable-pub-sub` set to `true`, `--audit-connection` set to `audit`, `--audit-channel` set to `audit` on audit pod.

**Note:** Verify that after the audit pod is running there is a dapr sidecar injected and running along side `manager` container.

3. Create connection config to establish a connection.

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: audit
  namespace: gatekeeper-system
data:
  provider: "dapr"
  config: |
    {
      "component": "pubsub"
    }
EOF
```
**Note:** Name of the connection configMap must match the value of `--audit-connection` for it to be used by audit to publish violation. At the moment, now only one connection config can exists for audit.

4. Create the constraint templates and constraints, and make sure audit ran by checking constraints. If constaint status is updated with information such as `auditTimeStamp` or `totalViolations`, then audit has ran atleast once. Additionally, populated `TOTAL-VIOLATIONS` field for all constraints while lising constraints also indicates that audit has ran at least once.

```log
kubctl get constraint
NAME                 ENFORCEMENT-ACTION   TOTAL-VIOLATIONS
pod-must-have-test                        0
```


5. Finally, check the subscriber logs to see the violations received.

```log
kubectl logs pod/sub-57bd5d694-7lvzf -c go-sub -n fake-subscriber 
2023/07/18 20:16:41 Listening...
2023/07/18 20:37:20 main.PubsubMsg{ID:"2023-07-18T20:37:19Z", Details:map[string]interface {}{"missing_labels":[]interface {}{"test"}}, EventType:"violation_audited", Group:"constraints.gatekeeper.sh", Version:"v1beta1", Kind:"K8sRequiredLabels", Name:"pod-must-have-test", Namespace:"", Message:"you must provide labels: {\"test\"}", EnforcementAction:"deny", ConstraintAnnotations:map[string]string(nil), ResourceGroup:"", ResourceAPIVersion:"v1", ResourceKind:"Pod", ResourceNamespace:"nginx", ResourceName:"nginx-deployment-58899467f5-j85bs", ResourceLabels:map[string]string{"app":"nginx", "owner":"admin", "pod-template-hash":"58899467f5"}}
```
