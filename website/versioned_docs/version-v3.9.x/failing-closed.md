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

In a nutshell it's because it's a webhook, and because it's self-hosted. All REST servers require enough high-availabily infrastructure to satisfy their SLOs (see cloud availability zones / regions). Self-hosted webhooks create a circular dependency that has the potential to interfere with the self-healing Kubenetes usually provides. Any self-hosted admission webhook would be subject to these same concerns.
