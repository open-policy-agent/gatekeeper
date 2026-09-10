---
id: api
title: API Reference
---

This page lists the Custom Resource Definitions (CRDs) that Gatekeeper installs and documents the fields users most often configure. It is a human-readable companion to the OpenAPI schemas embedded in the CRDs under [`config/crd/bases`](https://github.com/open-policy-agent/gatekeeper/tree/master/config/crd/bases).

To inspect the live schema in a cluster:

```bash
kubectl get crds | grep gatekeeper
kubectl explain configs.config.gatekeeper.sh.spec
kubectl explain constrainttemplates.templates.gatekeeper.sh.spec
```

Constraint kinds (for example `K8sRequiredLabels`) are **not** shipped as static CRDs. Gatekeeper creates those CRDs at runtime when you apply a `ConstraintTemplate`. Their shared constraint fields are documented below.

## CRD catalog

| Kind | API group | Scope | Purpose |
| ---- | --------- | ----- | ------- |
| `ConstraintTemplate` | `templates.gatekeeper.sh` | Cluster | Defines Rego/CEL policy and the schema for a constraint kind |
| *Constraint* (dynamic) | `constraints.gatekeeper.sh` | Cluster | Instantiates a template with match criteria, enforcement mode, and parameters |
| `Config` | `config.gatekeeper.sh` | Namespaced | Gatekeeper configuration (sync, exemption matchers, validation traces) |
| `SyncSet` | `syncset.gatekeeper.sh` | Cluster | Declares additional GVKs for Gatekeeper to cache |
| `Assign` | `mutations.gatekeeper.sh` | Cluster | Mutate arbitrary object fields |
| `AssignMetadata` | `mutations.gatekeeper.sh` | Cluster | Mutate metadata labels/annotations |
| `AssignImage` | `mutations.gatekeeper.sh` | Cluster | Mutate container image strings |
| `ModifySet` | `mutations.gatekeeper.sh` | Cluster | Merge or prune list values |
| `ExpansionTemplate` | `expansion.gatekeeper.sh` | Cluster | Expand generator resources (for example Deployments) into child objects for policy |
| `Connection` | `connection.gatekeeper.sh` | Namespaced | Configures export drivers/connections |
| `Provider` | `externaldata.gatekeeper.sh` | Cluster | Registers an external data provider |
| `*PodStatus` kinds | `status.gatekeeper.sh` | Namespaced | Per-pod status reported by Gatekeeper controllers (read-only operational data) |

Related feature docs: [How to use Gatekeeper](howto.md), [Constraint Templates](constrainttemplates.md), [Mutation](mutation.md), [Expansion](expansion.md), [External Data](externaldata.md), [Sync](sync.md), [Export](export.md).

---

## ConstraintTemplate (`templates.gatekeeper.sh`)

Preferred version: `v1` (also served: `v1beta1`).

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.crd.spec.names.kind` | string | Kind name for constraints created from this template (for example `K8sRequiredLabels`) |
| `spec.crd.spec.names.shortNames` | []string | Optional short names for `kubectl` |
| `spec.crd.spec.validation.openAPIV3Schema` | object | OpenAPI v3 schema for the constraint `spec.parameters` field |
| `spec.crd.spec.validation.legacySchema` | bool | Enable legacy schema mode (default `false`) |
| `spec.targets[]` | []object | Policy targets (usually `admission.k8s.gatekeeper.sh`) |
| `spec.targets[].target` | string | Target identifier |
| `spec.targets[].rego` | string | Rego source (legacy single-engine field) |
| `spec.targets[].code[]` | []object | Multi-engine sources (`engine` + `source`), for example Rego or CEL |
| `status` | object | Template installation status observed by Gatekeeper |

See [Constraint Templates](constrainttemplates.md) for examples.

---

## Constraint resources (`constraints.gatekeeper.sh`)

Each installed `ConstraintTemplate` registers a cluster-scoped constraint CRD whose kind matches `spec.crd.spec.names.kind`. All constraints share the following fields regardless of parameters schema.

### `spec` fields

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.match` | object | Selects which objects the constraint applies to. Matchers are AND-ed. Empty/undefined match selects everything. |
| `spec.match.kinds` | []object | List of `{apiGroups, kinds}` groups. A resource needs one matching entry. |
| `spec.match.scope` | string | `*`, `Cluster`, or `Namespaced` (default `*`) |
| `spec.match.namespaces` | []string | Include only these namespaces (prefix globs like `kube-*` allowed) |
| `spec.match.excludedNamespaces` | []string | Exclude these namespaces (prefix globs allowed) |
| `spec.match.labelSelector` | object | Standard label selector (`matchLabels` / `matchExpressions`) on the object |
| `spec.match.namespaceSelector` | object | Label selector on the object's namespace (or the object itself if it is a Namespace) |
| `spec.match.name` | string | Object name or prefix glob |
| `spec.parameters` | object | Template-specific inputs; validated against the template OpenAPI schema |
| `spec.enforcementAction` | string | How violations are handled (see below) |
| `spec.scopedEnforcementActions` | []object | Per-enforcement-point actions when `enforcementAction` is `scoped` |

Details on matching: [How to use Gatekeeper](howto.md#the-match-field).

### `spec.enforcementAction`

| Value | Behavior |
| ----- | -------- |
| `deny` | **Default.** Admission requests that violate the constraint are rejected. |
| `dryrun` | Violations are recorded (for example on the constraint status during audit) but admission is not blocked. |
| `warn` | Admission is allowed; clients receive a warning (Kubernetes 1.19+). |
| `scoped` | Use `spec.scopedEnforcementActions` to choose different actions per [enforcement point](enforcement-points.md). |

See [Handling Constraint Violations](violations.md) and [Enforcement Points](enforcement-points.md).

### `spec.scopedEnforcementActions[]`

| Field | Type | Description |
| ----- | ---- | ----------- |
| `action` | string | `deny`, `warn`, or `dryrun` for the listed enforcement points |
| `enforcementPoints[]` | []object | Entries with `name` such as `validation.gatekeeper.sh`, `audit.gatekeeper.sh`, or `gator.gatekeeper.sh`. Use `*` for all points. |

### `status` fields (observed)

| Field | Description |
| ----- | ----------- |
| `status.enforced` | Whether Gatekeeper is enforcing the constraint |
| `status.auditTimestamp` | Last audit pass that reported violations |
| `status.violations[]` | Sample of recent violations (`enforcementAction`, `kind`, `name`, `namespace`, `message`, …) |
| `status.totalViolations` | Total violation count when reported by audit |

---

## Config (`config.gatekeeper.sh/v1alpha1`)

Namespaced configuration object (typically `gatekeeper-system/config`).

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.sync.syncOnly[]` | []object | If non-empty, only these `{group, version, kind}` entries are replicated into OPA for referential policies |
| `spec.match[]` | []object | Process-specific configuration such as namespace exemptions |
| `spec.match[].processes` | []string | Processes this match entry applies to (for example `audit`, `webhook`, `*`) |
| `spec.match[].excludedNamespaces` | []string | Namespaces excluded for those processes (wildcards supported) |
| `spec.validation.traces[]` | []object | Admission trace requests for debugging (`user`, `kind`, optional `dump`) |
| `spec.readiness.statsEnabled` | bool | Enable readiness tracker stats |

See [Syncing](sync.md) and [Exempting Namespaces](exempt-namespaces.md).

---

## SyncSet (`syncset.gatekeeper.sh/v1alpha1`)

Cluster-scoped list of GVKs to cache. The effective sync set is the union of all `SyncSet` objects plus `Config.spec.sync.syncOnly`.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.gvks[]` | []object | Entries with `group`, `version`, and `kind` |

---

## Mutation CRDs (`mutations.gatekeeper.sh`)

Common mutation fields:

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.applyTo[]` | []object | GVKs the mutation schema applies to (`groups`, `versions`, `kinds`). Required for most mutators. |
| `spec.match` | object | Same style of matchers as constraints (limit which objects are mutated) |
| `spec.location` | string | Object path to mutate, for example `spec.containers[name: main].image` |
| `spec.parameters` | object | Mutator-specific options |
| `spec.parameters.pathTests[]` | []object | Optional `subPath` + `condition` (`MustExist` / `MustNotExist`) checks before mutating |

### Assign (`v1`)

| Field | Description |
| ----- | ----------- |
| `spec.parameters.assign` | Value assignment (`value` holds the concrete value to set) |

### AssignMetadata (`v1`)

Mutates only `metadata.labels` / `metadata.annotations` paths.

| Field | Description |
| ----- | ----------- |
| `spec.parameters.assign` | Assignment for the metadata field |

### AssignImage (`v1alpha1`)

| Field | Description |
| ----- | ----------- |
| `spec.parameters.assignDomain` | Image registry domain (no trailing slash) |
| `spec.parameters.assignPath` | Image path/repository component |
| `spec.parameters.assignTag` | Tag or digest; must start with `:` or `@` |

### ModifySet (`v1`)

| Field | Description |
| ----- | ----------- |
| `spec.parameters.operation` | `merge` (default) or `prune` |
| `spec.parameters.values.fromList` | List values to merge into or prune from the location |

See [Mutation](mutation.md).

---

## ExpansionTemplate (`expansion.gatekeeper.sh/v1beta1`)

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.applyTo[]` | []object | Generator resource GVKs to expand (for example Deployment, Job) |
| `spec.templateSource` | string | Field on the generator used as the pod/template source (often `spec.template`) |
| `spec.generatedGVK` | object | `{group, version, kind}` of the generated resource (for example Pod) |
| `spec.enforcementAction` | string | Optional override for enforcement on expanded resources; empty defers to the constraint |

See [Expansion](expansion.md).

---

## Connection (`connection.gatekeeper.sh/v1alpha1`)

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.driver` | string | Export driver name (for example `dapr`, `disk`) |
| `spec.config` | object | Driver-specific configuration (preserved unknown fields) |

See [Export](export.md).

---

## Provider (`externaldata.gatekeeper.sh`)

Registers an external data provider HTTP service. Typical fields:

| Field | Description |
| ----- | ----------- |
| `spec.url` | Provider endpoint URL (must use the `https://` prefix) |
| `spec.timeout` | Request timeout when querying the provider |
| `spec.caBundle` | Optional base64-encoded TLS CA bundle in PEM format |

See [External Data](externaldata.md) for the full provider API and examples.

---

## Status CRDs (`status.gatekeeper.sh`)

Gatekeeper writes namespaced status objects (for example `ConstraintPodStatus`, `ConstraintTemplatePodStatus`, `MutatorPodStatus`) so each controller pod can report health and errors. These are operational resources; they are not usually created or edited by users.

---

## Viewing schemas and examples

* CRD YAML with full OpenAPI schemas: [`config/crd/bases`](https://github.com/open-policy-agent/gatekeeper/tree/master/config/crd/bases)
* Helm-packaged CRDs: [`charts/gatekeeper/crds`](https://github.com/open-policy-agent/gatekeeper/tree/master/charts/gatekeeper/crds)
* End-to-end samples: [Examples](examples.md) and the [demo manifests](https://github.com/open-policy-agent/gatekeeper/tree/master/demo)
