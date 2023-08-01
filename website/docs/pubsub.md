---
id: pubsub
title: Consuming violations using Pubsub
---

`Feature State`: Gatekeeper version v3.13+ (alpha)

> ❗ This feature is alpha, subject to change (feedback is welcome!).

## Description

This feature pushes audit violations to a pubsub service. Users can subscribe to pubsub service to consume violations.

> To gain insights into different methods of obtaining audit violations and the respective trade-offs for each approach, please refer to [Reading Audit Results](audit.md#reading-audit-results).

## Enabling Gatekeeper to export audit violations

Install prerequisites such as a pubsub tool, a message broker etc.

### Setting up audit with pubsub enabled

In the audit deployment, set the `--enable-pub-sub` flag to `true` to publish audit violations. Additionally, `--audit-connection` and `--audit-channel` flags must be set to allow audit to publish violations. `--audit-connection` must be set to the name of the connection config, and `--audit-channel` must be set to name of the channel where violations should get published.

Create a connection configMap that supplies [a provider-specific configuration](pubsub-driver-walkthrough.md#how-to-use-different-providers) for a connection to get established. For instance, to establish a connection that uses Dapr to publish messages this configMap is appropriate:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: audit-pubsub-connection
  namespace: gatekeeper-system
data:
  provider: "dapr"
  config: |
    {
      "component": "pubsub"
    }
```

- `provider` field determines which tool/driver should be used to establish a connection. Valid values are: `dapr`
- `config` field is a json object that configures how the connection is made. E.g. which queue messages should be sent to.

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

    To install Dapr with specific requirements and configuration, please refer to [Dapr docs](https://docs.dapr.io/operations/hosting/kubernetes/kubernetes-deploy/).
    > Dapr is installed with mtls enabled by default, for more details on the same plaase refer to [Dapr security](https://docs.dapr.io/operations/security/mtls/#setting-up-mtls-with-the-configuration-resource).

2. Install Redis

    Please refer to [this](https://docs.dapr.io/getting-started/tutorials/configure-state-pubsub/#step-1-create-a-redis-store) guide to install Redis.

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

2. Install Gatekeeper with `--enable-pub-sub` set to `true`, `--audit-connection` set to `audit-pubsub-connection`, `--audit-channel` set to `audit` on audit pod.


```shell
echo 'auditPodAnnotations: {dapr.io/enabled: "true", dapr.io/app-id: "audit", dapr.io/metrics-port: "9999"}' > .tmp/annotations.yaml # auditPodAnnotations is used to add annotations required by Dapr to inject sidecar to audit pod
helm install gatekeeper/gatekeeper --name-template=gatekeeper --namespace gatekeeper-system \
--set audit.enablePubsub=true \
--set audit.connection=audit-pubsub-connection \
--set audit.channel=audit \
--values .tmp/annotations.yaml
```

**Note:** Verify that after the audit pod is running there is a dapr sidecar injected and running along side `manager` container.

3. Create connection config to establish a connection.

```shell
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: audit-pubsub-connection
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
