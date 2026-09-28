---
id: performance-tuning
title: Performance Tuning
---

Below we go into some of the considerations and options for performance tuning Gatekeeper.

# General Performance

## GOMAXPROCS

[GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS) sets the number of threads golang uses.
Gatekeeper uses [automaxprocs](https://github.com/uber-go/automaxprocs) to default this value
to the CPU limit set by the linux cgroup (i.e. the limits passed to the Kubernetes container).

This value can be overridden by setting a `GOMAXPROCS` environment variable.

Generally speaking, too many threads can lead to CPU throttling, which can increase webhook jitter
and can result in not enough available CPU per operation, which can lead to increased latency.

# Webhook Performance

## Max Serving Threads

The `--max-serving-threads` command line flag caps the number of concurrent goroutines that are
calling out to policy evaluation at any one time. This can be important for two reasons:

* Excessive numbers of serving goroutines can lead to CPU starvation, which means there is not enough
  CPU to go around per goroutine, causing requests to time out.

* Each serving goroutine can require a non-trivial amount of RAM, which will not be freed until the
  request is finished. This can increase the maximum memory used by the process, which can lead to
  OOMing.

By default, the number of webhook threads is capped at the value of `GOMAXPROCS`. If your policies mostly
rely on blocking calls (e.g. calling out to external services via `http.send()` or via external data), CPU
starvation is less of a risk, though memory scaling could still be a concern.

Playing around with this value may help maximize the throughput of Gatekeeper's validating webhook.

# Audit

## Audit Interval

The `--audit-interval` flag is used to configure how often audit runs on the cluster.

The time it takes for audit to run is dependent on the size of the cluster, any throttling the K8s
API server may do, and the number and complexity of policies to be evaluated. As such, determining
the ideal audit interval is use-case-specific.

If you have overlapping audits, the following things can happen:

* There will be parallel calls to the policy evaluation backend, which can result in increased
  RAM usage and CPU starvation, leading to OOMs or audit sessions taking longer per-audit than
  they otherwise would.

* More requests to the K8s API server. If throttled, this can increase the time it takes for an audit
  to finish.

* A newer audit run can pre-empt the reporting of audit results of a previous audit run on the `status` field
  of individual constraints. This can lead to constraints not having violation results in their `status` field.
  Reports via stdout logging should be unaffected by this.

Ideally, `--audit-interval` should be set long enough that no more than one audit is running at any time, though
occasional overlap should not be harmful.

## Constraint Violations Limit

Memory usage will increase/decrease as `--constraint-violations-limit` is increased/decreased.

## Audit Chunk Size

The `--audit-chunk-size` flags tells Gatekeeper to request lists of objects from the API server to be paginated
rather than listing all instances at once. Setting this can reduce maximum memory usage, particularly if you
have a cluster with a lot of objects of a specific kind, or a particular kind that has very large objects (say config maps).

One caveat about `--audit-chunk-size` is that the K8s API server returns a resumption token for list requests. This
token is only valid for a short window (~O(minutes)) and the listing of all objects for a given kind must be completed
before that token expires. Decreasing `--audit-chunk-size` should decrease maximum memory usage, but may also lead
to an increase in requests to the API server. In cases where this leads to throttling, it's possible the resumption token
could expire before object listing has completed.

## Match Kind Only

The `--audit-match-kind-only` flag can be helpful in reducing audit runtime, outgoing API requests and memory usage
if your constraints are only matching against a specific subset of kinds, particularly if there are large volumes
of config that can be ignored due to being out-of-scope. Some caveats:

* If the bulk of the K8s objects are resources that are already in-scope for constraints, the benefit will be mitigated

* If a constraint is added that matches against all kinds (say a label constraint), the benefit will be eliminated. If
  you are relying on this flag, it's important to make sure all constraints added to the cluster have `spec.match.kind`
  specified. 