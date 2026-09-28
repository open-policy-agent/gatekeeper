---
id: vendor-specific
title: Cloud and Vendor Specific Fixes
---

## Running on private GKE Cluster nodes

By default, firewall rules restrict the cluster master communication to nodes only on ports 443 (HTTPS) and 10250 (kubelet). Although Gatekeeper exposes its service on port 443, GKE by default enables `--enable-aggregator-routing` option, which makes the master to bypass the service and communicate straight to the POD on port 8443.

Two ways of working around this:

- create a new firewall rule from master to private nodes to open port `8443` (or any other custom port)
  - https://cloud.google.com/kubernetes-engine/docs/how-to/private-clusters#add_firewall_rules
- make the pod to run on privileged port 443 (need to run pod as root, or have `NET_BIND_SERVICE` capability)
  - update Gatekeeper deployment manifest spec:
    - add `NET_BIND_SERVICE` to `securityContext.capabilities.add` to allow binding on privileged ports as non-root
    - update port from `8443` to `443`
    ```yaml
    containers:
    - args:
      - --port=443
      ports:
      - containerPort: 443
        name: webhook-server
        protocol: TCP
      securityContext:
        capabilities:
          drop: ["all"]
          add: ["NET_BIND_SERVICE"]
    ```

## Running on OpenShift 4.x

When running on OpenShift, the `anyuid` scc must be used to keep a restricted profile but being able to set the UserID.

In order to use it, the following section must be added to the gatekeeper-manager-role Role:

```yaml
- apiGroups:
  - security.openshift.io
  resourceNames:
    - anyuid
  resources:
    - securitycontextconstraints
  verbs:
    - use
```

With this restricted profile, it won't be possible to set the `container.seccomp.security.alpha.kubernetes.io/manager: runtime/default` annotation. On the other hand, given the limited amount of privileges provided by the anyuid scc, the annotation can be removed.
