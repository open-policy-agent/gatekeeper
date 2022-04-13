---
id: customize-startup
title: Customizing Startup Behavior
---

## Allow retries when adding objects to OPA

Gatekeeper's webhook servers undergo a bootstrapping period during which they are unavailable until the initial set of resources (constraints, templates, synced objects, etc...) have been ingested. This prevents Gatekeeper's webhook from validating based on an incomplete set of policies. This wait-for-bootstrapping behavior can be configured.

The `--readiness-retries` flag defines the number of retry attempts allowed for an object (a Constraint, for example) to be successfully added to OPA.  The default is `0`.  A value of `-1` allows for infinite retries, blocking the webhook until all objects have been added to OPA.  This guarantees complete enforcement, but has the potential to indefinitely block the webhook from serving requests.

## Enable profiling using `pprof`

The `--enable-pprof` flag enables an HTTP server for profiling using the [pprof](https://pkg.go.dev/net/http/pprof) library. By default, it serves to `localhost:6060` but the port can be customized with the `--pprof-port` flag.

## Disable certificate generation and rotation for Gatekeeper's webhook

By default, Gatekeeper uses [`open-policy-agent/cert-controller`](https://github.com/open-policy-agent/cert-controller) to handle the webhook's certificate rotation and generation. If you want to use a third-party solution, you may disable the cert-controller feature using `--disable-cert-rotation`.

## Disable OPA built-in functions

The `--disable-opa-builtin` flag disables specific [OPA built-ins functions](https://www.openpolicyagent.org/docs/v0.37.2/policy-reference/#built-in-functions).

## [Alpha] Emit admission and audit events

The `--emit-admission-events` flag enables the emission of all admission violations as Kubernetes events in the Gatekeeper namespace. This flag is in alpha stage and it is set to `false` by default.

The `--emit-audit-events` flag enables the emission of all audit violation as Kubernetes events in the Gatekeeper namespace. This flag is in alpha stage and it is set to `false` by default.

There are three types of events that are emitted by Gatekeeper when the above flags are enabled:

| Event              | Description                                                             |
| ------------------ | ----------------------------------------------------------------------- |
| `FailedAdmission`  | The Gatekeeper webhook denied the admission request (default behavior). |
| `WarningAdmission` | When `enforcementAction: warn` is specified in the constraint.          |
| `DryrunViolation`  | When `enforcementAction: dryrun` is specified in the constraint.        |
| `AuditViolation`   | A violation is detected during an audit.                                |

> ❗ Warning: if the same constraint and violating resource tuple was emitted for [more than 10 times in a 10-minute rolling interval](https://github.com/kubernetes/kubernetes/blob/v1.23.3/staging/src/k8s.io/client-go/tools/record/events_cache.go#L429-L438), the Kubernetes event recorder will aggregate the events, e.g.
> ```
> 39s         Warning   FailedAdmission   namespace/test      (combined from similar events):  Admission webhook "validation.gatekeeper.sh" denied request, Resource Namespace: , Constraint: ns-must-have-gk, Message: you must provide labels: {"gatekeeper"}
> ```
> Gatekeeper might burst 25 events about an object, but limit the refill rate to 1 new event every 5 minutes. This will help control the long-tail of events for resources that are always violating the constraint.

## [Beta] Enable mutation logging and annotations

The `--log-mutations` flag enables logging of mutation events and errors.

The `--mutation-annotations` flag adds the following two annotations to mutated objects:

| Annotation                  | Value                                                                                                                                                         |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gatekeeper.sh/mutation-id` | The UUID of the mutation.                                                                                                                                     |
| `gatekeeper.sh/mutations`   | A list of comma-separated mutations in the format of `<MutationType>/<MutationNamespace>/<MutationName>:<MutationGeneration>` that are applied to the object. |
