---
id: pubsub
title: Pubsub
---

`Feature State`: Gatekeeper version v3.13+ (alpha)

> ❗ This feature is still in alpha stage, so the final form can still change (feedback is welcome!).

## Motivation

Prior to this feature, there were two ways to get audit violations. One is to look at constraints status and the other is to look at audit pod logs and get the logged audit violations. Both of these approach have limitations as described below.

Limitations of getting audit violations from constraint status:

- To reduce in memory consumption of audit and avoid violating etcd limit of 1.5MB per resource, gatekeeper only reports up-to 500 violations on constraint template. In other words, one might not get all the violations from looking at constraint.

Limitations of getting audit violations from audit logs:

- It could be difficult to parse audit logs and look for violation messages as violation logs would be scrambled together with other log statements. Additionally, when there are huge number of violations, the pod logs might get pruned and it might not be possible to get the logs for whole audit. In other words, it is possible to look at logs to fetch audit violation and still not get all the violations that were caught during audit.

This feature that uses publish and subscribe model, allows Gatekeeper to export audit violations over a broker that can be consumed by a subscriber independently. Therefore, it allows users to get all the audit violations.

## Enabling Gatekeeper to export audit violations

Install any prerequisites such as a pubsub tool, a message broker etc. To understand an example please refer to [quick-start](#quick-start-with-publishing-violations-using-dapr-and-redis) section below.

### Setting up audit with pubsub enabled

Set the `--enable-pub-sub` flag to `true` to publish audit violations. Additionally, `--audit-connection` and `--audit-channel` flags must be set to allow audit to publish violations. `--audit-connection` must be set to the name of the connection config, and `--audit-channel` must be set to name of the channel where violations should get published.

Create a connection configMap that supplies appropriate set of configurations for a connection to get established. For instance, to establish a connection that uses Dapr to publish messages this configMap is appropriate:

```
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

- `provider` field in `configMap.data` determined which tool/driver should be used to establish a connection.
- `config` field in `configMap.data` is a json data that allows users to pass appropriate information to establish connection to use respective provider.

**Note:** As of now, only Dapr driver is available to use and hence an example of pubsub set up with Dapr is described below.

### Quick start with publishing violations using Dapr and Redis

#### Prerequisites

1. Install Dapr

    ```
    helm repo add dapr https://dapr.github.io/helm-charts/
    helm upgrade --install dapr dapr/dapr --version=1.10 --namespace dapr-system --create-namespace --wait --debug
    ```

    To install dapr with specific requirements and configuration, please refer to [dapr docs](https://docs.dapr.io/getting-started/). 
    
    > Dapr is installed with mtls enabled by default, for more details on the same plaase refer to [dapr security](https://docs.dapr.io/operations/security/mtls/#setting-up-mtls-with-the-configuration-resource).

2. Install Redis

    ```
    helm repo add bitnami https://charts.bitnami.com/bitnami
    helm upgrade --install redis bitnami/redis --namespace default --set image.tag=7.0-debian-11 --wait --debug
    ```

    > To install redis with TLS, please refer to [this](https://docs.bitnami.com/kubernetes/infrastructure/redis-cluster/administration/enable-tls/) doc.

#### Configure a fake subscriber to receive violations

1. Create `fake-subscriber` namespace and redis secret

```
kubectl create ns fake-subscriber
kubectl get secret redis --namespace=default -o yaml | sed 's/namespace: .*/namespace: fake-subscriber/' | kubectl apply -f -
```

2. Create Dapr pubsub component
```
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
> Please use [this guide](https://docs.dapr.io/reference/components-reference/supported-state-stores/setup-redis/) to properly configure redis pubsub component for Dapr.

3. Deploy subscriber application
```
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

1. Create Dapr pubsub component and redis secret in `gatekeeper-system` (i.e. the namespace where gatekeeper will be installed).

```
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

2. Install gatekeeper with `--enable-pub-sub` set to `true`, `--audit-connection` set to `audit`, `--audit-channel` set to `audit` on audit pod.

**Note:** Verify that after the audit pod is running there is a dapr sidecar injected and running along side `manager` container.

3. Create connection config to establish a connection.

```
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
**Note:** Name of the connection configMap must match the value of `--audit-connection` for it to be used by audit to publish violation. Hence, right now only one connection config can exists for audit.

4. Create the constraint templates and constraints, and let the audit run it's course.

Finally, check the subscriber logs to see the violations received.
