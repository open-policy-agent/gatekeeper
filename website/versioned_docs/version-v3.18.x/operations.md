---
id: operations
title: Operations
---

Gatekeeper is flexible in how it can be deployed. If desired, core pieces of functionality can be broken
out to be run in different pods. This allows Gatekeeper to accommodate needs like running in a monolithic pod
in order to avoid overhead, or running each operation in a separate pod to make scaling individual operations
easier and to limit the impact of operational issues with any one operation (e.g. if audit is running in its
own pod, audit running out of memory will not affect the validation webhook).

Gatekeeper achieves this through the concept of `Operations`, which can be enabled via the `--operation`
command line flag. To enable multiple operations this flag can be defined multiple times. If no `--operation`
flag is provided, all functionality will be enabled by default.

# Operations

Below are the operations Gatekeeper supports

## Validating Webhook

__--operation key:__ `webhook`

This operation serves the validating webhook that Kubernetes' API server calls as part of the admission process.

### Required Behaviors

At a high level, this requires:

* Ingesting constraint templates
* Creating CRDs for a corresponding constraint template
* Ingesting constraints
* Reporting the status of ingested constraints/templates
* Watching and syncing resources specified by the `Config` resource to support referential constraints
* Running the HTTP validating webhook service
   * In addition to validating incoming requests against policy, this webhook also validates incoming Gatekeeper resources
* Running the namespace label validating webhook service (required to lock down namespaceSelector-type webhook exemptions)

### Permissions Required

* The ability to read all `ConstraintTemplate` objects
* The ability to create CRDs (unfortunately RBAC doesn't have the syntax to scope this down to just CRDs in the `constraints.gatekeeper.sh` group)
* The ability to read all `Constraint` resources (members of the group `constraints.gatekeeper.sh`)
* The ability to create `ConstraintTemplatePodStatus` objects in Gatekeeper's namespace
* The ability to create `ConstraintPodStatus` objects in Gatekeeper's namespace
* The ability to write to the `Config` object in Gatekeeper's namespace
* The ability to read all objects (optionally this can be scoped down to resources listed for syncing in the `Config`)
* If certificates are managed by Gatekeeper's embedded cert controller (which can be disabled), then Gatekeeper will need
  write permissions to its `ValidatingWebhookConfiguration`
  * It will also need the ability to write to the webhook secret in Gatekeeper's namespace
* If you have events enabled, you will need permissions to create events in Gatekeeper's namespace


## Mutating Webhook

__--operation key:__ `mutation-webhook`

This operation serves the mutating webhook that Kubernetes' API server calls as part of the admission process.

### Required Behaviors

At a high level, this requires:

* Ingesting Mutator objects
* Reporting the status of ingested mutator objects
* Running the HTTP mutating webhook service

### Permissions Required

* The ability to read all objects in the group `mutations.gatekeeper.sh` (mutators)
* The ability to create `MutatorPodStatus` objects in Gatekeeper's namespace
* If certificates are managed by Gatekeeper's embedded cert controller (which can be disabled), then Gatekeeper will need
  write permissions to its `MutatingWebhookConfiguration`
  * It will also need the ability to write to the webhook secret in Gatekeeper's namespace

## Audit

__--operation key:__ `audit`

This operation runs the audit process, which periodically evaluates existing resources against policy, reporting
any violations it discovers. To limit traffic to the API server and to avoid contention writing audit results
to constraints, audit should run as a singleton pod.

### Required Behaviors

At a high level, this requires:

* Listing all objects on the cluster to scan them for violations
* Ingesting constraint templates
* Creating CRDs for a corresponding constraint template
* Ingesting constraints
* Reporting the status of ingested constraints/templates
* Watching and syncing resources specified by the `Config` resource to support referential constraints
* Writing audit results back to constraints (subject to a cap on # of results per constraint)

### Permissions Required

* The ability to read all objects in the cluster (this can be scoped down if you are not interested in auditing/syncing all objects)
* The ability to read all `ConstraintTemplate` objects
* The ability to create CRDs (unfortunately RBAC doesn't have the syntax to scope this down to just CRDs in the `constraints.gatekeeper.sh` group)
* The ability to write to all `Constraint` resources (members of the group `constraints.gatekeeper.sh`)
* The ability to create `ConstraintTemplatePodStatus` objects in Gatekeeper's namespace
* The ability to create `ConstraintPodStatus` objects in Gatekeeper's namespace
* The ability to write to the `Config` object in Gatekeeper's namespace
* If you have events enabled, you will need permissions to create events in Gatekeeper's namespace

## Status

__--operation key:__ `status`

Gatekeeper uses an emergent consensus model, where individual pods do not need to talk with each other
in order to provide its functionality. This allows for scalability, but means we should not write status
to resources directly due to the risk of write contention, which could increase network traffic exponentially
relative to the number of pods. Instead, each pod gets its own, private status resource that it alone writes
to. The Status operation aggregates these status resources and writes them to the status field of the appropriate
object for the user to consume. Without this operation, the `status` field of constraints and constraint templates
would be blank.

In order to do its job (eliminating write contention) effectively, the Status operation should be run as a
singleton.

### Required Behaviors

At a high level, this requires:

* Reading the Constraint[Template]PodStatus resources
* Writing aggregated results to the `status` fields of constraints/templates

### Permissions Required

* The ability to write to all `ConstraintTemplate` objects
* The ability to write to all `Constraint` resources (members of the group `constraints.gatekeeper.sh`)
* The ability to read `ConstraintTemplatePodStatus` objects in Gatekeeper's namespace
* The ability to read `ConstraintPodStatus` objects in Gatekeeper's namespace

## Mutation Status

__--operation key:__ `mutation-status`

Because users may not want to install mutation CRDs if they do not want to use the feature, and because
trying to watch a Kind that doesn't exist would cause errors, Gatekeeper splits mutation status into a
separate operation. It behaves like the Status operation, except it only applies for mutation resources.

### Required Behaviors

At a high level, this requires:

* Reading mutator pod status resources
* Writing aggregated results to the `status` fields of mutators

### Permissions Required

* The ability to write to all objects in the group `mutations.gatekeeper.sh` (mutators)
* The ability to read `MutatorPodStatus` objects in Gatekeeper's namespace

## Mutation Controller

__--operation key:__ `mutation-controller`

This operation runs the process responsible for ingesting and registering
mutators. `mutation-controller` is run implicitly with the `mutation-webhook`
and `mutation-status` operations, and is redundant if any of the 2
aforementioned operations are already specified. 

If the `webhook` or `audit` operation is used in isolation without the `mutation-webhook`
or `mutation-status` operations, then the `mutation-controller` operation is
required for mutation to work with [workload expansion](workload-resources.md).

### Required Behaviors:

At a high level, this requires:

* Ingesting Mutator objects

### Permissions Required

* The ability to read all objects in the group `mutations.gatekeeper.sh` (mutators)

# A Note on Permissions

"Create" implies the `create` and `delete` permissions in addition to the permissions implied by "Read" and "Write".

"Write" implies the `update` permission in addition to the permissions implied by "Read".

"Read" implies the `get`, `list`, and `watch` permissions. In some cases, like scraping audit results,
`watch` is unnecessary, but does not substantially increase the power delegated to the service account
under the theory that a `watch` is simply a more efficient version of polling `list`.
