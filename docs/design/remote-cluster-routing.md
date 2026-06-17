# Remote Cluster Status Resource Routing Design

**Status**: Proposal implemented
**Author**: Abhishek Sheth
**Created**: 2026-05-18
**Last Updated**: 2026-06-17

## Overview
In remote cluster mode, Gatekeeper runs on a **local cluster** but enforces policies on a separate **remote cluster** (connected via `--kubeconfig`). PodStatus resources need to live on the local cluster because:

1. **Garbage collection needs both objects on the same cluster:** Each PodStatus has an OwnerReference pointing to the Gatekeeper pod. If the PodStatus is on a different cluster than the pod, Kubernetes can't find the owner and GC breaks.
2. **Automatic cleanup on pod restart:** When a Gatekeeper pod restarts, Kubernetes GC deletes the old PodStatus objects automatically.


All other resources (ConstraintTemplates, Constraints, Config, etc.) stay on the remote cluster.


## Design

A routing layer wraps the manager's cache and client. On every read or write, it looks at the resource's API group and sends it to the right cluster:

- `status.gatekeeper.sh/*` → **local cluster**
- Everything else → **remote cluster**

The routing check is simple:

```go
func isLocalClusterResource(gvk schema.GroupVersionKind) bool {
    return gvk.Group == "status.gatekeeper.sh"
}
```

This covers all 7 PodStatus types across `v1alpha1` and `v1beta1`.

### Components

**`routingCache`** (`pkg/routing/cache.go`): wraps `cache.Cache`, constructed via `NewRoutingCache(remoteCluster, localCluster, scheme)`. Every cache operation (`Get`, `List`, `GetInformer`, `GetInformerForKind`, `RemoveInformer`, `IndexField`) checks the resource type and picks the right underlying cache. It starts and syncs both caches. In non-remote mode, both point to the same cache.

**`routingClient`** (`pkg/routing/client.go`): wraps `client.Client`, constructed via `NewRoutingClient(remoteCluster, localCluster, scheme)`. It routes all reads (`Get`, `List`) and writes (`Create`, `Update`, `Delete`, `Patch`, `DeleteAllOf`, `Apply`) by resource type. `Status()` and `SubResource()` are also routed (via a `routingSubResourceClient`) by the parent object's GVK, so subresource writes on PodStatus objects reach the local cluster too. The local cluster client is cache-backed (it reuses the manager-provided cache reader), so routed PodStatus reads are served from the local cache rather than hitting the API server on every call.

**`resolveGVK`** (`pkg/routing/routing.go`): figures out the GVK of an object. It first checks if the object already has a GVK set, then falls back to looking it up in the scheme (typed objects). If it can't determine the GVK, it returns an error. The shared `routeIsLocal` helper wraps `resolveGVK` and `isLocalClusterResource`, and the error propagates up through `clientFor`/`cacheFor` so the operation fails loudly rather than silently routing to the wrong cluster.

### main.go

```go
// localClusterConfig = where the pod runs (the local/management cluster).
// Only built when remote cluster mode is enabled; nil otherwise.
var localClusterConfig *rest.Config
if controller.RemoteClusterEnabled() {
    localClusterConfig, _ = rest.InClusterConfig()
}

// The manager's primary config is the remote (target) cluster.
mgr, _ := ctrl.NewManager(config, ctrl.Options{
    NewCache:  newCacheFunc(scheme, localClusterConfig),   // nil in non-remote mode
    NewClient: newClientFunc(scheme, localClusterConfig),  // nil in non-remote mode
})
```

When `localClusterConfig` is nil (non-remote mode), these return nil and controller-runtime uses its defaults. Nothing changes.

The local cluster cache only watches the `gatekeeper-system` namespace to keep RBAC permissions minimal.

## Design Decisions

1. **Routing happens at the manager level**: Controllers, audit, and webhooks don't know about routing. No controller code changes were needed.

2. **Route by API group, not resource name**: The API group is stable across versions. No need to maintain a list of resource names.

3. **OwnerReferences are always set**: Previously, remote mode skipped setting OwnerReferences (since they wouldn't work cross-cluster). Now that PodStatus lives on the same cluster as the pod, OwnerReferences always work.

4. **Both caches must sync before ready**: `WaitForCacheSync` waits for both caches, so the manager won't serve traffic until both are ready.

5. **Error on unknown GVK**: If `resolveGVK` can't determine the resource type (e.g. unregistered type, empty GVK), the routing layer returns an error and the operation fails. An unresolved GVK means the routing decision can't be made, so failing loudly is safer.

## RBAC Requirements (Local Cluster)

The local cluster needs minimal permissions in the `gatekeeper-system` namespace:

| Resource | Verbs |
|----------|-------|
| `pods` | `get` (for OwnerReference lookup) |
| `status.gatekeeper.sh/*` (all PodStatus types) | `get`, `list`, `watch`, `create`, `update`, `patch`, `delete` |

These are scoped to the `gatekeeper-system` namespace via a `Role` + `RoleBinding` (not a `ClusterRole`). No cluster-wide permissions are needed on the local cluster.

## Files Changed

| File | What it does |
|------|-------------|
| `pkg/routing/routing.go` | Routing decision logic (`isLocalClusterResource`, `resolveGVK`) |
| `pkg/routing/cache.go` | `routingCache` / `NewRoutingCache`, routes cache reads and informers |
| `pkg/routing/client.go` | `routingClient` / `NewRoutingClient`, routes client reads, writes, and subresources; `routeIsLocal` helper |
| `main.go` | Builds the local cluster config, passes routing factories to the manager |
| `pkg/controller/controller.go` | `RemoteClusterEnabled()` helper |
| `apis/status/v1*/...` | OwnerReferences always set (removed conditional skip) |

## Non-Remote Mode

When `--enable-remote-cluster` is not set:
- `localClusterConfig` is nil
- Routing factories return nil
- Manager uses its default cache and client
- Everything goes to the single cluster
- Behavior is identical to before this change
