---
id: customize-startup
title: Customizing Startup Behavior
---

## Allow retries when adding objects to OPA

Gatekeeper's webhook servers undergo a bootstrapping period during which they are unavailable until the initial set of resources (constraints, templates, synced objects, etc...) have been ingested. This prevents Gatekeeper's webhook from validating based on an incomplete set of policies. This wait-for-bootstrapping behavior can be configured.

The `--readiness-retries` flag defines the number of retry attempts allowed for an object (a Constraint, for example) to be successfully added to OPA.  The default is `0`.  A value of `-1` allows for infinite retries, blocking the webhook until all objects have been added to OPA.  This guarantees complete enforcement, but has the potential to indefinitely block the webhook from serving requests.