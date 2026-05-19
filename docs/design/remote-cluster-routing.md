# Remote Cluster Status Resource Routing Design

**Status**: Proposal implemented
**Author**: Abhishek Sheth
**Created**: 2026-05-18
**Last Updated**: 2026-05-18

## Overview
In remote cluster mode, Gatekeeper runs on a **management cluster** but enforces policies on a separate **target cluster** (connected via `--kubeconfig`). PodStatus resources need to live on the management cluster because:

1. **Garbage collection needs both objects on the same cluster** — Each PodStatus has an OwnerReference pointing to the Gatekeeper pod. If the PodStatus is on a different cluster than the pod, Kubernetes can't find the owner and GC breaks.
2. **Automatic cleanup on pod restart** — When a Gatekeeper pod restarts, Kubernetes GC deletes the old PodStatus objects automatically.


All other resources (ConstraintTemplates, Constraints, Config, etc.) stay on the target cluster.

## Design

A routing layer wraps the manager's cache and client. On every read or write, it looks at the resource's API group and sends it to the right cluster:

- `status.gatekeeper.sh/*` → **management cluster**
- Everything else → **target cluster**

The routing check is simple:

```
IsManagementResource(gvk) = gvk.Group == "status.gatekeeper.sh"
```

This covers all 7 PodStatus types across `v1alpha1` and `v1beta1`.

### Components

**`RoutingCache`** (`pkg/routing/cache.go`) — Wraps `cache.Cache`. Every cache operation (`Get`, `List`, `GetInformer`, etc.) checks the resource type and picks the right underlying cache. Starts and syncs both caches. In non-remote mode, both point to the same cache.

**`RoutingClient`** (`pkg/routing/client.go`) — Wraps `client.Client`. Routes all reads (`Get`, `List`) and writes (`Create`, `Update`, `Delete`, `Patch`, `DeleteAllOf`) by resource type. The read overrides are needed because controller-runtime's default client reads from the cache — without them, reads would go to the target cache while writes go to the management API server.

**`ResolveGVK`** (`pkg/routing/routing.go`) — Figures out the GVK of an object. First checks if the object already has a GVK set, then falls back to looking it up in the scheme (typed objects). If it can't determine the GVK, it falls back to the target cluster.

### main.go

```go
mgmtConfig, _ = rest.InClusterConfig()  // management cluster = where the pod runs

mgr, _ := ctrl.NewManager(targetConfig, ctrl.Options{
    NewCache:  newCacheFunc(scheme, mgmtConfig),   // nil in non-remote mode
    NewClient: newClientFunc(scheme, mgmtConfig),   // nil in non-remote mode
})
```

When `mgmtConfig` is nil (non-remote mode), these return nil and controller-runtime uses its defaults. Nothing changes.

The management cache only watches the `gatekeeper-system` namespace to keep RBAC permissions minimal.

## Design Decisions

1. **Routing happens at the manager level**: Controllers, audit, and webhooks don't know about routing. No controller code changes were needed.

2. **Route by API group, not resource name**: The API group is stable across versions. No need to maintain a list of resource names.

3. **OwnerReferences are always set**: Previously, remote mode skipped setting OwnerReferences (since they wouldn't work cross-cluster). Now that PodStatus lives on the same cluster as the pod, OwnerReferences always work, so the skip was removed.

4. **Both caches must sync before ready**: `WaitForCacheSync` waits for both caches, so the manager won't serve traffic until both are ready.

5. **Fallback to target cluster on unknown GVK**: If `ResolveGVK` can't determine the resource type (e.g. unregistered type, empty GVK), the operation falls back to the target cluster. This preserves existing behavior and avoids breaking non-status operations.

## RBAC Requirements (Management Cluster)

The management cluster needs minimal permissions in the `gatekeeper-system` namespace:

| Resource | Verbs |
|----------|-------|
| `pods` | `get` (for OwnerReference lookup) |
| `status.gatekeeper.sh/*` (all PodStatus types) | `get`, `list`, `watch`, `create`, `update`, `patch`, `delete` |

These are scoped to the `gatekeeper-system` namespace via a `Role` + `RoleBinding` (not a `ClusterRole`). No cluster-wide permissions are needed on the management cluster.

## Files Changed

| File | What it does |
|------|-------------|
| `pkg/routing/routing.go` | Routing decision logic (`IsManagementResource`, `ResolveGVK`) |
| `pkg/routing/cache.go` | `RoutingCache` — routes cache reads and informers |
| `pkg/routing/client.go` | `RoutingClient` — routes client reads and writes |
| `main.go` | Creates management config, passes routing factories to manager |
| `pkg/controller/controller.go` | `RemoteClusterEnabled()` helper |
| `apis/status/v1*/...` | OwnerReferences always set (removed conditional skip) |

## Non-Remote Mode

When `--enable-remote-cluster` is not set:
- `mgmtConfig` is nil
- Routing factories return nil
- Manager uses its default cache and client
- Everything goes to the single cluster
- Behavior is identical to before this change
