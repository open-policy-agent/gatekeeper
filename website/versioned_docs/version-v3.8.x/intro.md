---
id: intro
title: Introduction
sidebar_label: Introduction
slug: /
---

## Goals

Every organization has policies. Some are essential to meet governance and legal requirements. Others help ensure adherence to best practices and institutional conventions. Attempting to ensure compliance manually would be error-prone and frustrating. Automating policy enforcement ensures consistency, lowers development latency through immediate feedback, and helps with agility by allowing developers to operate independently without sacrificing compliance.

Kubernetes allows decoupling policy decisions from the inner workings of the API Server by means of [admission controller webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/), which are executed whenever a resource is created, updated or deleted. Gatekeeper is a validating (mutating TBA) webhook that enforces CRD-based policies executed by [Open Policy Agent](https://github.com/open-policy-agent/opa), a policy engine for Cloud Native environments hosted by CNCF as a [graduated project](https://www.cncf.io/projects/open-policy-agent-opa/).

In addition to the `admission` scenario, Gatekeeper's audit functionality allows administrators to see what resources are currently violating any given policy.

Finally, Gatekeeper's engine is designed to be portable, allowing administrators to detect and reject non-compliant commits to an infrastructure-as-code system's source-of-truth, further strengthening compliance efforts and preventing bad state from slowing down the organization.

## How is Gatekeeper different from OPA?

Compared to using [OPA with its sidecar kube-mgmt](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html) (aka Gatekeeper v1.0), Gatekeeper introduces the following functionality:

   * An extensible, parameterized policy library
   * Native Kubernetes CRDs for instantiating the policy library (aka "constraints")
   * Native Kubernetes CRDs for extending the policy library (aka "constraint templates")
   * Audit functionality

### Admission Webhook Fail-Open by Default

Currently Gatekeeper is defaulting to using `failurePolicy​: ​Ignore` for admission request webhook errors. The impact of
this is that when the webhook is down, or otherwise unreachable, constraints will not be
enforced. Audit is expected to pick up any slack in enforcement by highlighting invalid
resources that made it into the cluster.

If you would like to switch to fail closed, please see our [documentation](failing-closed.md) on how to do so and some things you should consider before doing so.
