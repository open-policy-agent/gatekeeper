---
id: customize-startup
title: Customizing Startup Behavior
---

## Allow retries when adding objects to OPA

Gatekeeper's webhook servers undergo a bootstrapping period during which they are unavailable until the initial set of resources (constraints, templates, synced objects, etc...) have been ingested. This prevents Gatekeeper's webhook from validating based on an incomplete set of policies. This wait-for-bootstrapping behavior can be configured.

The `--readiness-retries` flag defines the number of retry attempts allowed for an object (a Constraint, for example) to be successfully added to OPA.  The default is `0`.  A value of `-1` allows for infinite retries, blocking the webhook until all objects have been added to OPA.  This guarantees complete enforcement, but has the potential to indefinitely block the webhook from serving requests.

## Enable profiling using `pprof`

The `--enable-pprof` flag enables a HTTP server for profiling using the [pprof](https://pkg.go.dev/net/http/pprof) library. By default, it serves to `localhost:6060` but the port can be customized with the `--pprof-port` flag.

## Disable certificate generation and rotation for Gatekeeper's webhook

By default, Gatekeeper uses [`open-policy-agent/cert-controller`](https://github.com/open-policy-agent/cert-controller) to handle the webhook's certificate rotation and generation by. If you want to use a third-party solution, you may disable the cert-controller feature using `--disable-cert-rotation`.

## [Alpha] Emit admission and audit events

The `--emit-admission-events` flag enables the emission of all admission violations as Kubernetes events in the Gatekeeper namespace. This flag in alpha stage and it is set to `false` by default.

The `--emit-audit-events` flag enables the emission of all audit violation as Kubernetes events in the Gatekeeper namespace. This flag in alpha stage and it is set to `false` by default.

> Note: if the same constraint/audit was violated for more than 10 times in a 10-minute period, the Kubernetes event recorder will aggregate the events, e.g.
> ```
> 39s         Warning   FailedAdmission   namespace/test      (combined from similar events):  Admission webhook "validation.gatekeeper.sh" denied request, Resource Namespace: , Constraint: ns-must-have-gk, Message: you must provide labels: {"gatekeeper"}
> ```
