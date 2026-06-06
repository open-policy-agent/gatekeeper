---
id: remote-cluster
title: Remote Cluster Mode
---

`Feature State`: Gatekeeper version v3.23+ (alpha)

> ⚠️ **Alpha feature.** Remote cluster mode is under active development. Flags,
> chart values, and the setup flow described here may change between releases.
> Some of the wiring (certificates, cross-cluster networking) is still manual —
> see [Known limitations](#known-limitations).

Remote cluster mode lets a single Gatekeeper instance run in one cluster (the
**management cluster**) while enforcing policy on a separate cluster (the
**target cluster**).

When `--enable-remote-cluster` is set, Gatekeeper:

- Connects to the **target cluster** (via `--kubeconfig`) for everything policy
  related: ConstraintTemplates, Constraints, the resources being validated, and
  admission review traffic.
- Keeps its own **PodStatus** resources (`status.gatekeeper.sh`) on the
  **management cluster**, alongside the Gatekeeper pod. Because the pod and its
  status objects live in the same cluster, OwnerReferences work natively and
  Kubernetes garbage collection cleans them up automatically when a pod is
  replaced.


## Prerequisites

- Two Kubernetes clusters: a management cluster and a target cluster.
- The target cluster's API server must be able to reach a Gatekeeper webhook
  endpoint running in the management cluster over HTTPS.
- A kubeconfig for the target cluster, mounted into the Gatekeeper pod in the
  management cluster.
- `kubectl` access to both clusters.

## Step 1 — Install CRDs

Status CRDs go on the **management cluster**, user-facing policy CRDs go on the
**target cluster**.

```bash
# Status CRDs → management cluster
kubectl --context <mgmt> apply \
  -f config/crd/bases/status.gatekeeper.sh_*.yaml

# Policy / user-facing CRDs → target cluster
kubectl --context <target> apply -f config/crd/bases/
kubectl --context <target> apply \
  -f charts/gatekeeper/crds/provider-customresourcedefinition.yaml
```

## Step 2 — Deploy Gatekeeper to the management cluster

Deploy Gatekeeper to the management cluster with remote cluster mode enabled and
`--kubeconfig` pointing at the target cluster.

Key flags:

```bash
--enable-remote-cluster            # Enable remote cluster mode
--kubeconfig=/etc/kubeconfig/kubeconfig  # Kubeconfig for the target cluster
```


Mount the target kubeconfig as a Secret, for example:

```bash
kubectl --context <mgmt> create secret generic target-kubeconfig \
  --from-file=kubeconfig=/path/to/target-kubeconfig.yaml \
  -n gatekeeper-system
```

then reference it via `extraVolumes` / `extraVolumeMounts` (or a custom
manifest) so it lands at the path passed to `--kubeconfig`.

### RBAC on the management cluster

Gatekeeper needs permission on the **management cluster** to read its own pod
identity and to manage status resources:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: gatekeeper-manager-role
  namespace: gatekeeper-system
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
- apiGroups: ["status.gatekeeper.sh"]
  resources: ["*"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

## Step 3 — Webhook certificates and trust

The target cluster's API server calls the Gatekeeper webhook over HTTPS, so it
must trust the certificate that endpoint serves. There are two approaches.

For real clusters, manage the webhook serving certificate with a tool
 and inject it via the chart.

### Local / kind experimentation only

For a throwaway local setup (for example, two `kind` clusters), you can generate
a self-signed CA and serving certificate by hand. The certificate's SANs must
cover the address the target API server uses to reach the management cluster.

> 🚫 **Do not use self-signed, hand-generated certificates in production.** This
> path exists only to make local experimentation easy.

```bash
# CA
openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -subj "/CN=gatekeeper-ca" \
  -days 365 -out ca.crt

# Serving cert (add the reachable address as a SAN, e.g. the management
# node IP or the in-cluster service DNS name)
openssl genrsa -out tls.key 2048
# ...create a CSR with subjectAltName and sign it with the CA above...

kubectl --context <mgmt> create secret generic gatekeeper-webhook-server-cert \
  --from-file=tls.crt=tls.crt \
  --from-file=tls.key=tls.key \
  --from-file=ca.crt=ca.crt \
  -n gatekeeper-system
```

A complete, runnable example of the local flow lives in the remote cluster
end-to-end test under
[`.github/workflows/remote-cluster-e2e.yaml`](https://github.com/open-policy-agent/gatekeeper/blob/master/.github/workflows/remote-cluster-e2e.yaml).

## Step 4 — Configure the webhook on the target cluster

Create a `ValidatingWebhookConfiguration` on the **target cluster** whose
`clientConfig.url` points at the management-cluster Gatekeeper endpoint, with the
issuing CA in `caBundle`:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: gatekeeper-validating-webhook-configuration
webhooks:
- name: validation.gatekeeper.sh
  admissionReviewVersions: ["v1", "v1beta1"]
  clientConfig:
    url: https://<management-endpoint>:<port>/v1/admit
    caBundle: <base64 CA bundle>
  rules:
  - apiGroups: ["*"]
    apiVersions: ["*"]
    operations: ["CREATE", "UPDATE"]
    resources: ["namespaces"]
  failurePolicy: Fail
  sideEffects: None
  namespaceSelector:
    matchExpressions:
    - key: kubernetes.io/metadata.name
      operator: NotIn
      values: ["kube-system", "kube-public", "kube-node-lease", "gatekeeper-system"]
```

Use a routable `url` for your environment (a Service of type `LoadBalancer`, an
Ingress, or — for local clusters — a node address/NodePort).

## Step 5 — Smoke test

Apply a sample policy to the **target cluster** and confirm that PodStatus
objects appear only on the **management cluster**.

```bash
# Apply a template + constraint to the target cluster
kubectl --context <target> apply -f \
  https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/test/bats/tests/remote-cluster/k8srequiredlabels_template.yaml
kubectl --context <target> apply -f \
  https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/test/bats/tests/remote-cluster/k8srequiredlabels_constraint.yaml
```

Verify status resources are created on the **management cluster**:

```bash
kubectl --context <mgmt> get constrainttemplatepodstatuses,constraintpodstatuses \
  -n gatekeeper-system
```

Verify there are **no** PodStatus resources on the target cluster:

```bash
kubectl --context <target> get constrainttemplatepodstatuses \
  -n gatekeeper-system 2>/dev/null
# (expected: no resources found)
```

Verify enforcement on the target cluster — a Namespace missing the required
label should be rejected:

```bash
# Rejected (missing required label)
kubectl --context <target> create ns test-ns-reject

# Allowed (label present)
kubectl --context <target> apply -f - <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns-allow
  labels:
    test-label: "yes"
EOF
```

The aggregated status (`.status.byPod`) on the ConstraintTemplate and Constraint
on the target cluster should also become populated once the management-cluster
pod reports in.

## Known limitations

- Cross-cluster certificate and webhook wiring is manual. Helm knobs
  (`enableRemoteCluster`, `kubeconfig`, `extraVolumes`/`extraVolumeMounts`,
  `validatingWebhookURL`, `externalCertInjection`) configure the
  management-cluster half; the target-cluster webhook and CA bundle must be set
  up separately as shown above.

## Migration from previous versions

If you previously ran `--enable-remote-cluster` in a version that stored status
resources on the target cluster, clean up the now-orphaned resources once:

```bash
kubectl delete constrainttemplatepodstatuses,constraintpodstatuses,mutatorpodstatuses,expansiontemplatepodstatuses,configpodstatuses,providerpodstatuses,connectionpodstatuses \
  -n gatekeeper-system --all --context <target-cluster-context>
```

After upgrading, status resources are managed automatically on the management
cluster.
