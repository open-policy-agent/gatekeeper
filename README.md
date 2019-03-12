# gatekeeper

[![Build Status](https://travis-ci.org/open-policy-agent/gatekeeper.svg?branch=master)](https://travis-ci.org/open-policy-agent/gatekeeper) [![Docker Repository on Quay](https://quay.io/repository/open-policy-agent/gatekeeper/status "Docker Repository on Quay")](https://quay.io/repository/open-policy-agent/gatekeeper)

## Warning: Restructing underway

This is a new project that is undergoing heavy restructuring.  The policy format, architecture, interfaces, and code layout are all subject to change.

If you need OPA-style admission control right now, we recommend using the [OPA Kubernetes Admission Control tutorial](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html).
The policies you see there will closely resemble the ones Gatekeeper will support once restructuring is complete.

## Want to help?
Join us to help define the direction and implementation of this project!

- Join the [`#kubernetes-policy`](https://openpolicyagent.slack.com/messages/CDTN970AX)
  channel on [OPA Slack](https://slack.openpolicyagent.org/).

- Join [weekly meetings](https://docs.google.com/document/d/1A1-Q-1OMw3QODs1wT6eqfLTagcGmgzAJAjJihiO3T48/edit)
  to discuss development, issues, use cases, etc.

- Use [GitHub Issues](https://github.com/open-policy-agent/gatekeeper/issues)
  to file bugs, request features, or ask questions asynchronously.


## Goals

Every organization has some rules. Some of these are essential to meet governance, and legal requirements and other are based on learning from past experience and not repeating the same mistakes. These decisions cannot tolerate human response time as they need near a real-time action. Services that are policy enabled to make the organization agile and are essential for long-term success as they are more adaptable as violations and conflicts can be discovered consistently as they are not prone to human error.

Kubernetes allows decoupling complex logic such as policy decisions from the inner working of the API Server by means of [admission controller webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/). This webhooks are executed whenever a resource is created, updated or deleted and can be used to implement complex custom logic. `gatekeeper` is a mutating and a validating webhook that gets called for matching Kubernetes API server requests by the admission controller. Kubernetes also has another extension mechanism for general authorization decisions (not necessarily related to resources) which is called [authorization modules](https://kubernetes.io/docs/reference/access-authn-authz/authorization/). Usually, just the RBAC authorization module is used, but with `gatekeeper` it's possible to implement a blacklist in front of RBAC. `gatekeeper` uses Open Policy Agent ([OPA](https://github.com/open-policy-agent/opa)), a policy engine for Cloud Native environments hosted by CNCF as a sandbox-level project.

Kubernetes compliance is enforced at the “runtime” via tools such as network policy and pod security policy. [gatekeeper](https://github.com/Azure/gatekeeper) extends the compliance enforcement at “create” event not at “run“ event. For example, a kubernetes service could answer questions like :

* Can we whitelist / blacklist registries.
* Not allow conflicting hosts for ingresses.
* Label objects based on a user from a department.

In addition to the `admission` scenario  it helps answer the `audit` question such as:

* What are the policies that my cluster is violating.

In the `authorization` scenario it's possible to block things like `kubectl get`, `kubectl port-forward` or even non-resource requests (all authorized request can be blocked).



