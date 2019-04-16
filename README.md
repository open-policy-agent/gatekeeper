# Gatekeeper

[![Build Status](https://travis-ci.org/open-policy-agent/gatekeeper.svg?branch=master)](https://travis-ci.org/open-policy-agent/gatekeeper) [![Docker Repository on Quay](https://quay.io/repository/open-policy-agent/gatekeeper/status "Docker Repository on Quay")](https://quay.io/repository/open-policy-agent/gatekeeper)

## Warning: Restructing underway

This is a new project that is under development.  The architecture, interfaces, and code layout are all subject to change, though the policy syntax and ConstraintTemplate spec schema should be stable enough for alpha.

If you need OPA-style admission control right now, we recommend using the [OPA Kubernetes Admission Control tutorial](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html).

## Want to help?
Join us to help define the direction and implementation of this project!

- Join the [`#kubernetes-policy`](https://openpolicyagent.slack.com/messages/CDTN970AX)
  channel on [OPA Slack](https://slack.openpolicyagent.org/).

- Join [weekly meetings](https://docs.google.com/document/d/1A1-Q-1OMw3QODs1wT6eqfLTagcGmgzAJAjJihiO3T48/edit)
  to discuss development, issues, use cases, etc.

- Use [GitHub Issues](https://github.com/open-policy-agent/gatekeeper/issues)
  to file bugs, request features, or ask questions asynchronously.


## Goals

Every organization has policies. Some are essential to meet governance and legal requirements. Others help ensure adherance to best practices and institutional conventions. Attempting to ensure compliance manually would be error-prone and frustrating. Automating policy enforcement ensures consistency, lowers development latency through immediate feedback, and helps with agility by allowing developers to operate independently without sacrificing compliance.

Kubernetes allows decoupling policy decisions from the inner workings of the API Server by means of [admission controller webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/), which are executed whenever a resource is created, updated or deleted. Gatekeeper is a validating (mutating TBA) webhook that enforces CRD-based policies executed by [Open Policy Agent](https://github.com/open-policy-agent/opa), a policy engine for Cloud Native environments hosted by CNCF as a sandbox-level project.

In addition to the `admission` scenario, Gatekeeper's audit functionality allows administrators to see what resources are currently violating any given policy.

Finally, Gatekeeper's engine is designed to be portable, allowing administrators to detect and reject non-compliant commits to an infrastructure-as-code system's source-of-truth, further strenthening compliance efforts and preventing bad state from slowing down the organization.

## Installation Instructions

WARNING: It is not recommended to install Gatekeeper on a production cluster. The project is in alpha and may or may not uninstall cleanly.

### Installation

Currently the most reliable way of installing Gatekeeper is to build and install from HEAD:

   * Make sure [Kubebuilder is installed](https://book.kubebuilder.io/getting_started/installation_and_setup.html)
   * Clone the Gatekeeper repo to your local system
   * Make sure you have a container registry you can write to that is readable by the target cluster
   * cd to the repository directory
   * run `make docker-build REPOSITORY=<YOUR DESIRED DESTINATION DOCKER IMAGE>`
   * run `make docker-push-release REPOSITORY=<YOUR DESIRED DESTINATION DOCKER IMAGE>`
   * make sure your kubectl context is set to the desired installation cluster
   * run `make deploy`

### Uninstallation

Note that this is not a clean uninstall. There may be some CRDs that are not cleaned up

   * cd to the repository directory
   * run `make uninstall`

## How to Use Gatekeeper

### Constraint Templates

Before you can define a constraint, you must first define a `ConstraintTemplate`, which describes both the Rego that enforces the constraint and the schema of the constraint. The schema of the constraint allows an admin to fine-tune the behavior of a constraint, much like arguments to a function.

Here is an example constraint template that requires all labels described by the constraint to be present:

```yaml
apiVersion: templates.gatekeeper.sh/v1alpha1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
        listKind: K8sRequiredLabelsList
        plural: k8srequiredlabels
        singular: k8srequiredlabels
      validation:
        # Schema for the `parameters` field
        openAPIV3Schema:
          properties:
            labels:
              type: array
              items: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels

        deny[{"msg": msg, "details": {"missing_labels": missing}}] {
          provided := {label | input.review.object.metadata.labels[label]}
          required := {label | label := input.constraint.spec.parameters.labels[_]}
          missing := required - provided
          count(missing) > 0
          msg := sprintf("you must provide labels: %v", [missing])
        }
```

### Constraints

Constraints are then used to inform Gatekeeper that the admin wants a ConstraintTemplate to be enforced, and how. This constraint uses the `K8sRequiredLabels` constraint above to make sure the `gatekeeper` label is defined on all namespaces:

```yaml
apiVersion: constraints.gatekeeper.sh/v1alpha1
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
```

Note the `match` field, which defines the scope of objects to which a given constraint will be applied. It supports the following matchers:

   * `kinds` accepts a list of objects with `apiGroups` and `kinds` fields that list the groups/kinds of objects to which the constraint will apply. If multiple groups/kinds objects are specified, only one match is needed for the resource to be in scope.
   * `namespaces` is a list of namespace names. If defined, a constraint will only apply to resources in a listed namespace.
   * `labelSelector` is a standard Kubernetes label selector.

Note that if multiple matchers are specified, a resource must satisfy each top-level matcher (`kinds`, `namespaces`, etc.) to be in scope. Each top-level matcher has its own semantics for what qualifies as a match. An empty matcher is deemed to be inclusive (matches everything).

### Replicating Data

Kubernetes data can be replicated into OPA via the sync config. Currently resources defined in `syncOnly` will be synced into OPA. Updating `syncOnly` should dynamically update what objects are synced. Below is an example:

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  sync:
    syncOnly:
      - group: ""
        version: "v1"
        kind: "Namespace"
      - group: ""
        version: "v1"
        kind: "Pod"
```

Once data is synced into OPA, rules can be written to do things like enforce that an ingress's hostname is unique among all ingresses.

## Kick The Tires

The `demo` directory has an example of simple constraints, templates and configs to play with.