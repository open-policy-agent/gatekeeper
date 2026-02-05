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

The `--disable-opa-builtin` flag disables specific [OPA built-ins functions](https://www.openpolicyagent.org/docs/v0.37.2/policy-reference/#built-in-functions). Starting with v3.8.0, Gatekeeper disables the `http.send` built-in function by default. For more information, please see [external data](./externaldata.md#motivation).

## [Alpha] Emit admission and audit events

The `--emit-admission-events` flag enables the emission of all admission violations as Kubernetes events. This flag is in alpha stage and it is set to `false` by default.

The `--emit-audit-events` flag enables the emission of all audit violation as Kubernetes events. This flag is in alpha stage and it is set to `false` by default.

The `--admission-events-involved-namespace` flag controls which namespace admission events will be created in. When set to `true`, admission events will be created in the namespace of the object violating the constraint. If the object has no namespace (ie. cluster scoped resources), they will be created in the namespace Gatekeeper is installed in. Setting to `false` will cause all admission events to be created in the Gatekeeper namespace.

The `--audit-events-involved-namespace` flag controls which namespace audit events will be created in. When set to `true`, audit events will be created in the namespace of the object violating the constraint. If the object has no namespace (ie. cluster scoped resources), they will be created in the namespace Gatekeeper is installed in. Setting to `false` will cause all audit events to be created in the Gatekeeper namespace.

There are four types of events that are emitted by Gatekeeper when the emit event flags are enabled:

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

> ❗ Note that this will break the idempotence requirement that Kubernetes sets for mutation webhooks. See the [Kubernetes docs here](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#idempotence) for more details

## [Alpha] Remote Cluster Mode for Gatekeeper

The `--enable-remote-cluster` flag enables Gatekeeper to run in a local (management) cluster while enforcing policies on a separate target cluster specified via `--kubeconfig`. This is designed for hosted control plane architectures where the target cluster's API server runs within the management cluster.

### When to Use

Use remote cluster mode when:
- Gatekeeper runs in a local (management) cluster
- You want to enforce policies on a remote target cluster

### Configuration

```bash
--enable-remote-cluster            # Enable remote cluster mode
--kubeconfig=/path/to/target.yaml  # Kubeconfig for target cluster
```

### RBAC Requirements

Gatekeeper needs permissions to read Pods in the **local cluster** (to resolve its own pod identity):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: gatekeeper-manager-role
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
```

### Orphan Resource Cleanup

In remote cluster mode, status resources don't have OwnerReferences (since the pod doesn't exist in the target cluster). When Gatekeeper pods restart, their old status resources become orphaned.

To find orphaned status resources, compare the `gatekeeper.sh/pod` label against running pods. You should check all status resource types: `constrainttemplatepodstatuses`, `constraintpodstatuses`, `mutatorpodstatuses`, `expansiontemplatepodstatuses`, `configpodstatuses`, `providerpodstatuses`, and `connectionpodstatuses`.

```bash
# List all pod names referenced in status resources (repeat for each status type)
kubectl get constrainttemplatepodstatuses -o jsonpath='{.items[*].metadata.labels.gatekeeper\.sh/pod}' | tr ' ' '\n' | sort -u

# Compare with running Gatekeeper pods in local cluster
kubectl get pods -n gatekeeper-system -l control-plane=controller-manager -o name
```

To clean up orphaned resources after identifying old pod names:

```bash
# Delete status resources for a specific old pod
OLD_POD="gatekeeper-controller-manager-old-xyz"
kubectl delete constrainttemplatepodstatuses,constraintpodstatuses,mutatorpodstatuses,expansiontemplatepodstatuses,configpodstatuses,providerpodstatuses,connectionpodstatuses -l gatekeeper.sh/pod=$OLD_POD
```

## Other Configuration Options

For the complete list of configuration flags for your specific version of Gatekeeper, run the Gatekeeper binary with the `--help` flag. For example:

`docker run openpolicyagent/gatekeeper:v3.10.0-beta.0 --help`

To ensure you are seeing all relevant flags, be sure the image tag (`:3.10.0-beta.0` above) corresponds with the version of Gatekeeper you are running.
