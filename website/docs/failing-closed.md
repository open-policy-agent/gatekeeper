---
id: failing-closed
title: Failing Closed
---

## Admission Webhook Fail-Open by Default

Currently Gatekeeper is defaulting to using `failurePolicy: Ignore` for admission request webhook errors. The impact of this is that when the webhook is down, or otherwise unreachable, constraints will not be enforced. Audit is expected to pick up any slack in enforcement by highlighting invalid resources that made it into the cluster.

Here we discuss how to configure Gatekeeper to fail closed and some factors you may want to consider before doing so.

## How to Fail Closed

If you installed Gatekeeper via the manifest, the only needed change is to set the `failurePolicy` field of Gatekeeper's `ValidatingWebhookConfiguration` to `Fail`. For example:


```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  labels:
    gatekeeper.sh/system: "yes"
  name: gatekeeper-validating-webhook-configuration
webhooks:
- admissionReviewVersions:
  - v1beta1
  clientConfig:
    caBundle: SOME_CERT
    service:
      name: gatekeeper-webhook-service
      namespace: gatekeeper-system
      path: /v1/admit
      port: 443
  failurePolicy: Fail
  matchPolicy: Exact
  name: validation.gatekeeper.sh
  namespaceSelector:
    matchExpressions:
    - key: admission.gatekeeper.sh/ignore
      operator: DoesNotExist
  rules:
  - apiGroups:
    - '*'
    apiVersions:
    - '*'
    operations:
    - CREATE
    - UPDATE
    resources:
    - '*'
    scope: '*'
  sideEffects: None
  timeoutSeconds: 3
- admissionReviewVersions:
  - v1beta1
  clientConfig:
    caBundle: SOME_CERT
    service:
      name: gatekeeper-webhook-service
      namespace: gatekeeper-system
      path: /v1/admitlabel
      port: 443
  failurePolicy: Fail
  matchPolicy: Exact
  name: check-ignore-label.gatekeeper.sh
  namespaceSelector: {}
  objectSelector: {}
  rules:
  - apiGroups:
    - ""
    apiVersions:
    - '*'
    operations:
    - CREATE
    - UPDATE
    resources:
    - namespaces
    scope: '*'
  sideEffects: None
  timeoutSeconds: 3
```

If you installed Gatekeeper via any other method (Helm chart, operator), please consult the documentation for that method.

## Considerations

Here are some factors you may want to consider before configuring Gatekeeper to fail closed.

### Admission Deadlock

#### Example

It is possible to put the cluster in a state where automatic self-healing is impossible. Imagine you delete every `Node` in your cluster. This will kill all running Gatekeeper servers, which means the webhook will fail. Because a request to add a `Node` is subject to admission validation, it cannot succeed until the webhook can serve. The webhook cannot serve until a `Node` is added. This circular dependency will need to be broken before the cluster's control plane can recover.

#### Mitigation

This can normally be mitigated by deleting the `ValidatingWebhookConfiguration`, per the [emergency procedure](emergency.md).

Note that it should always be possible to modify or delete the `ValidatingWebhookConfiguration` because Kubernetes does not make requests to edit webhook configurations subject to admission webhooks.

#### Potential Gotchas

If the existence of the webhook resource is enforced by some external process (such as an operator), that may interfere with the emergency recovery process. If this applies, it would be good to have a plan in place to deal with that scenario.

### Cluster Control Plane Availability

Because the webhook is being called for all K8s API server requests (under the default configuration), the availability of K8s's control plane becomes subject to the availability of the webhook. It is important to have an idea of your expected API server availability [SLO](https://en.wikipedia.org/wiki/Service-level_objective) and make sure Gatekeeper is configured to support that.

Below are some potential ways to do that and their gotchas.

#### Limit the Gatekeeper Webhook's Scope

It is possible to exempt certain namespaces from being subject to the webhook, or to only call the webhook for certain kinds. This could be one way to prevent the webhook from interfering with sensitive processes.

##### Potential Gotchas

It can be hard to say for certain that all critical resources have been exempted because dependencies can be non-obvious. Some examples:

- Exempting `kube-system` namespace is a good starting place, but what about cluster-scoped resources, like nodes? What about other potentially critical namespaces like `istio-system`?
- Some seemingly innocuous kinds can actually play a critical role in cluster operations. Did you know that a `ConfigMap` is used as the locking resource for some Kubernetes leader elections?

If you are relying on exempting resources to keep your cluster available, be sure you know all the critical dependencies of your cluster. Unfortunately this is very cluster-specific, so there is no general guidance to be offered here.

#### Harden Your Deployment

Gatekeeper attempts to be resilient out-of-the-box by running its webhook in multiple pods. You can take that work and adapt it to your cluster by adding the appropriate node selectors and scaling the number of nodes up or down as desired.

##### Impact of Scaling Nodes

Putting hard numbers on the impact scaling resources has on Gatekeeper's availability depends on the specifics of the underlying hardware of your cluster and how Gatekeeper is distributed across it, but there are some general themes:

- Increasing the number of webhook pods should increase QPS serving capacity
- Increasing the number of webhook pods tends to increase uptime of the service
- Increasing the number of webhook pods may increase the time it takes for a constraint to be enforced by all pods in the system

##### Potential Gotcha: Failure Domains

Increasing the number of pods increases the theoretical uptime of a system under the theory that if one pod goes down the other pods continue to serve and pick up the slack. This assumption fails if multiple pods fail at the same time due to the same root cause. This happens when multiple pods are in the same [failure domain](https://en.wikipedia.org/wiki/Failure_domain#:~:text=In%20computing%2C%20a%20failure%20domain,of%20infrastructure%20that%20could%20fail.).

Here are some common ways for two pods to be in the same failure domain:

- Running on the same node
- Running on the same physical host (e.g. multiple nodes are VMs backed by the same physical machine)
- Running on different physical hosts with the same network switch
- Running on different physical hosts with the same power supply
- Running on different physical hosts in the same rack

Different clusters may have different backing physical infrastructures and different risk tolerances. Because of this, there is no definitive list of failure domains or guidance on how that should affect your setup.

## Why Is This Hard?

In a nutshell it's because it's a webhook, and because it's self-hosted. All REST servers require enough high-availabily infrastructure to satisfy their SLOs (see cloud availability zones / regions). Self-hosted webhooks create a circular dependency that has the potential to interfere with the self-healing Kubernetes usually provides. Any self-hosted admission webhook would be subject to these same concerns.

## Detecting Fail-Open Admission

When `failurePolicy: Ignore` is set (the default), a request admitted because the webhook
call failed does **not** produce a Gatekeeper denial log. Gatekeeper logs and metrics alone
are not sufficient to detect all fail-open events.

### API Server Metric

The Kubernetes API server exposes `apiserver_admission_webhook_fail_open_count`, a counter
incremented each time an admission request is admitted due to a webhook call failure.

Filter by the `name` label to monitor Gatekeeper's webhook:

```promql
rate(apiserver_admission_webhook_fail_open_count{name="validation.gatekeeper.sh"}[5m]) > 0
```

#### Example Prometheus Alert

```yaml
groups:
  - name: gatekeeper
    rules:
      - alert: GatekeeperFailOpen
        expr: rate(apiserver_admission_webhook_fail_open_count{name="validation.gatekeeper.sh"}[5m]) > 0
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Gatekeeper webhook is failing open"
          description: >-
            Admission requests are being admitted without policy evaluation
            because the Gatekeeper webhook is unreachable.
```

> **Note:** This metric is served by the API server, not by Gatekeeper. On managed clusters
> (GKE, EKS, AKS) the API server metrics endpoint may not be directly accessible. Consult
> your provider's documentation for metric availability. A missing metric is **not** the same
> as a zero count — verify that the metric is being scraped before relying on it.

### Complementary Availability Signals

A ready pod or a populated `EndpointSlice` alone does not prove that the API server can
reach the webhook. Consider monitoring these signals together:

| Signal | What it proves | What it does not prove |
|--------|---------------|----------------------|
| Pod readiness (`kubectl get pods -n gatekeeper-system`) | Gatekeeper process is running | API server network path is healthy |
| `EndpointSlice` for `gatekeeper-webhook-service` | Service has backing endpoints | Traffic can traverse network policies and firewalls |
| `gatekeeper_validation_request_count` > 0 | Requests are reaching Gatekeeper | — |
| `apiserver_admission_webhook_fail_open_count` = 0 | No fail-open events observed | Metric is being scraped (verify independently) |

A drop in `gatekeeper_validation_request_count` to zero alongside a nonzero
`apiserver_admission_webhook_fail_open_count` confirms webhook unreachability.

### Audit vs. Admission Enforcement

[Audit](audit.md) evaluates existing resources periodically and reports violations after
the fact. It is a **detection** mechanism, not preventive enforcement or automatic
remediation. Resources that bypassed the webhook during a fail-open window will eventually
appear in audit results, but they have already been admitted into the cluster.

## Restricting Webhook Access with NetworkPolicy

The Gatekeeper Helm chart includes an optional `NetworkPolicy` for the controller-manager
pods. Enabling it can prevent ordinary tenant pods from reaching the webhook service
directly while preserving API server admission traffic.

### Enabling via Helm

Set `controllerManager.networkPolicy.enabled` to `true` and configure `ingress` rules
to allow traffic from the API server:

```yaml
controllerManager:
  networkPolicy:
    enabled: true
    ingress:
      - from:
          - namespaceSelector:
              matchLabels:
                kubernetes.io/metadata.name: kube-system
        ports:
          - protocol: TCP
            port: 8443
```

> **Important:** The correct `ingress.from` selector is topology-specific. API server
> traffic may originate from control-plane nodes, host-networked processes, or managed
> control-plane IP addresses. A `namespaceSelector` or `podSelector` is **not** universally
> sufficient.
>
> For managed clusters where the API server runs outside the cluster network, use an
> `ipBlock` CIDR:
>
> ```yaml
> controllerManager:
>   networkPolicy:
>     enabled: true
>     ingress:
>       - from:
>           - ipBlock:
>               cidr: <API_SERVER_CIDR>/32
>         ports:
>           - protocol: TCP
>             port: 8443
> ```

The chart template is defined in
[`gatekeeper-controller-manager-network-policy.yaml`](https://github.com/open-policy-agent/gatekeeper/blob/master/cmd/build/helmify/static/templates/gatekeeper-controller-manager-network-policy.yaml).

### Verification

After deploying the NetworkPolicy:

1. **Confirm the policy selects the correct pods:**

   ```shell
   kubectl get networkpolicy gatekeeper-controller-manager -n gatekeeper-system -o yaml
   ```

   Verify that `spec.podSelector.matchLabels` matches the labels on the controller-manager
   pods:

   ```shell
   kubectl get pods -n gatekeeper-system --show-labels
   ```

2. **Verify API server admission still works:**

   ```shell
   # Create a resource that should be denied by an existing constraint
   kubectl apply -f <violating-resource.yaml>
   # Expected: denied by Gatekeeper
   ```

3. **Verify tenant pod access is restricted:**

   ```shell
   # From a tenant namespace, attempt to reach the webhook directly
   kubectl run net-test --rm -it --image=curlimages/curl -- \
     curl -sk https://gatekeeper-webhook-service.gatekeeper-system.svc:443/v1/admit
   # Expected: connection timeout or refused
   ```

### Rollback

If the NetworkPolicy causes admission failures or increases fail-open events:

```shell
# Option 1: Delete the NetworkPolicy directly
kubectl delete networkpolicy gatekeeper-controller-manager -n gatekeeper-system

# Option 2: Disable via Helm
helm upgrade gatekeeper gatekeeper/gatekeeper \
  --set controllerManager.networkPolicy.enabled=false
```

> **Warning:** An incorrect NetworkPolicy under `failurePolicy: Ignore` causes silent
> fail-open admission. Under `failurePolicy: Fail` it causes rejected requests for all
> matching resources. Monitor `apiserver_admission_webhook_fail_open_count` immediately
> after applying changes.

### CNI and Host-Network Limitations

NetworkPolicy enforcement requires a CNI plugin that supports it (Calico, Cilium, Antrea,
etc.). Clusters using a CNI without policy support (e.g. Flannel alone) will accept the
`NetworkPolicy` resource but **will not enforce** it.

Host-networked pods and node-level processes (including the API server on self-hosted
clusters) may bypass `NetworkPolicy` depending on the CNI implementation. Verify
enforcement in your specific environment before relying on it.

Record the Kubernetes version, CNI plugin, and control-plane topology used during
validation. State limitations rather than assuming one portable policy works everywhere.

## Independent Enforcement Mechanisms

Gatekeeper is one layer of policy enforcement. For defense-in-depth, consider complementary
mechanisms that do not depend on the webhook:

- **[Pod Security Admission (PSA)](https://kubernetes.io/docs/concepts/security/pod-security-admission/)**:
  Built-in, namespace-level enforcement of Pod Security Standards. Effective protection
  depends on correctly configuring PSA labels (`enforce`, `audit`, `warn`) on all relevant
  namespaces.

- **[Validating Admission Policies (VAP)](validating-admission-policy.md)**:
  Kubernetes-native CEL-based policies that run in the API server without an external
  webhook. A CEL expression in a Gatekeeper `ConstraintTemplate` is evaluated by
  Gatekeeper's webhook — for native API server enforcement you need a separate
  `ValidatingAdmissionPolicy` and `ValidatingAdmissionPolicyBinding`.

The availability of native policy mechanisms does not remove the need to monitor
Gatekeeper's webhook for policies that are only enforced through Gatekeeper constraints.
