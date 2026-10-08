---
id: api
title: API Reference
---

This page lists Gatekeeper's Custom Resource Definitions (CRDs) and provides a quick reference for commonly configured fields in Gatekeeper v3.23.x. It is not an exhaustive schema reference: use the [schemas and examples](#viewing-schemas-and-examples) for complete field definitions and the linked feature guides for usage examples. Required fields and defaults below include Gatekeeper's validation rules, not only OpenAPI schema requirements.

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
| `Assign` | `mutations.gatekeeper.sh` | Cluster | Mutate object fields outside metadata |
| `AssignMetadata` | `mutations.gatekeeper.sh` | Cluster | Add missing metadata labels/annotations |
| `AssignImage` | `mutations.gatekeeper.sh` | Cluster | Mutate container image strings |
| `ModifySet` | `mutations.gatekeeper.sh` | Cluster | Merge or prune list values |
| `ExpansionTemplate` | `expansion.gatekeeper.sh` | Cluster | Expand generator resources (for example Deployments) into child objects for policy |
| `Connection` | `connection.gatekeeper.sh` | Namespaced | Configures audit export drivers/connections |
| `Provider` | `externaldata.gatekeeper.sh` | Cluster | Registers an external data provider |
| [`*PodStatus` kinds](#status-crds-statusgatekeepersh) | `status.gatekeeper.sh` | Namespaced | Per-pod status reported by Gatekeeper controllers (read-only operational data) |

---

## ConstraintTemplate (`templates.gatekeeper.sh`)

Preferred version: `v1` (also served: `v1beta1`, `v1alpha1`).

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.crd.spec.names.kind` | string | Required kind name for constraints created from this template (for example `K8sRequiredLabels`) |
| `spec.crd.spec.names.shortNames` | []string | Optional short names for `kubectl` |
| `spec.crd.spec.validation.openAPIV3Schema` | object | Optional schema for the constraint `spec.parameters` field; `v1` templates use a [structural schema](constrainttemplates.md#v1-constraint-template) |
| `spec.crd.spec.validation.legacySchema` | bool | Enable legacy schema mode; defaults to `false` in `v1`, but `true` in `v1alpha1` and `v1beta1` |
| `spec.targets[]` | []object | Required policy target definitions |
| `spec.targets[].target` | string | Required target identifier: `admission.k8s.gatekeeper.sh` |
| `spec.targets[].rego` | string | Legacy Rego policy source; alternatively use `code` |
| `spec.targets[].libs` | []string | Optional supporting libraries for the legacy Rego source |
| `spec.targets[].code[]` | []object | Engine-specific policy definitions, each with `engine` and `source` |
| `spec.targets[].code[].engine` | string | Required for each code entry: `Rego` or `K8sNativeValidation` (CEL) |
| `spec.targets[].code[].source` | object | Required engine-specific source; see [Rego v1](constrainttemplates.md#enable-opa-rego-v1-syntax-in-constrainttemplates) and [CEL sources](validating-admission-policy.md#policy-updates-to-add-vap-cel) |
| `status` | object | Read-only template installation status observed by Gatekeeper |

See [Constraint Templates](constrainttemplates.md) for examples and [engine selection and field precedence](constrainttemplates.md#field-precedence-in-constrainttemplate). Defining multiple engines does not mean that all of them are evaluated.

---

## Constraint resources (`constraints.gatekeeper.sh/v1beta1`)

Each installed `ConstraintTemplate` registers a cluster-scoped constraint CRD whose kind matches `spec.crd.spec.names.kind`. All constraints share the following fields regardless of parameters schema.

### `spec` fields

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.match` | object | Optional object selection. Matchers are AND-ed. Empty/undefined match selects everything. |
| `spec.match.kinds` | []object | List of `{apiGroups, kinds}` groups. A resource needs one matching entry. |
| `spec.match.scope` | string | `*`, `Cluster`, or `Namespaced` (default `*`) |
| `spec.match.namespaces` | []string | Include only these namespaces (prefix globs like `kube-*` allowed) |
| `spec.match.excludedNamespaces` | []string | Exclude these namespaces (prefix globs allowed) |
| `spec.match.labelSelector` | object | Standard label selector (`matchLabels` / `matchExpressions`) on the object |
| `spec.match.namespaceSelector` | object | Label selector on the object's namespace (or the object itself if it is a Namespace) |
| `spec.match.name` | string | Object name or prefix glob |
| `spec.match.source` | string | Target resource origin: `All`, `Generated`, or `Original` (default `All`); see [expansion matching](expansion.md#match-source) |
| `spec.parameters` | object | Template-specific inputs; required fields and types depend on the template's OpenAPI schema |
| `spec.enforcementAction` | string | Optional violation handling mode; defaults to `deny` (see below) |
| `spec.scopedEnforcementActions` | []object | Required when `enforcementAction` is `scoped`; ignored otherwise |

All match fields are optional. Namespace filters do not exclude non-Namespace cluster-scoped resources; set `spec.match.scope: Namespaced` to exclude those resources. See [matching semantics](howto.md#the-match-field).

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
| `action` | string | Required: `deny`, `warn`, or `dryrun` for the listed enforcement points |
| `enforcementPoints[]` | []object | Required non-empty list of enforcement points |
| `enforcementPoints[].name` | string | Required: `validation.gatekeeper.sh`, `audit.gatekeeper.sh`, `gator.gatekeeper.sh`, `vap.k8s.io`, or `*` for all points |

With `enforcementAction: scoped`, an enforcement point not named in any entry is excluded unless an entry names `*`. In particular, audit is not implicitly included. Native Kubernetes enforcement (`vap.k8s.io`) requires a CEL template and VAP/VAPBinding generation; see [Enforcement Points](enforcement-points.md#understanding-enforcement-points).

### `status` fields (observed)

| Field | Description |
| ----- | ----------- |
| `status.byPod[].enforced` | Enforcement status reported by each Gatekeeper controller pod |
| `status.auditTimestamp` | Timestamp of the latest audit run recorded in this constraint's status, including runs with zero violations |
| `status.violations[]` | Bounded list of violations from the latest recorded audit run (`enforcementAction`, `kind`, `name`, `namespace`, `message`, ...) |
| `status.totalViolations` | Total violation count from that audit run, including violations omitted from `status.violations` |

The violation list is limited by `--constraint-violations-limit` (default `20`); see [Reading Audit Results](audit.md#constraint-status).

---

## Config (`config.gatekeeper.sh/v1alpha1`)

Singleton configuration object. It must be named `config` in Gatekeeper's namespace (`gatekeeper-system` by default). Gatekeeper ignores Config resources with other names or namespaces.

All configuration sections below are optional.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.sync.syncOnly[]` | []object | `{group, version, kind}` entries to replicate for referential policies; combined with all SyncSets, not an override of them |
| `spec.match[]` | []object | Process-specific configuration such as namespace exemptions |
| `spec.match[].processes` | []string | `audit`, `webhook`, `sync`, `mutation-webhook`, or `*` for all processes; see [namespace exemptions](exempt-namespaces.md#exempting-namespaces-from-gatekeeper-using-config-resource) |
| `spec.match[].excludedNamespaces` | []string | Namespaces excluded for those processes (wildcards supported) |
| `spec.validation.traces[]` | []object | Admission trace requests; each needs `user` and `kind` (`group`, `version`, `kind`). Optional `dump: All` includes OPA state; see [Tracing](debug.md#tracing) |
| `spec.readiness.statsEnabled` | bool | Enable readiness tracker stats (default `false`) |

See [replication with Config](sync.md#replicating-data-with-config).

---

## SyncSet (`syncset.gatekeeper.sh/v1alpha1`)

Cluster-scoped list of GVKs to cache. The effective sync set is the union of all `SyncSet` objects plus `Config.spec.sync.syncOnly`.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.gvks[]` | []object | Entries with `group`, `version`, and `kind` |

A resource remains cached while any SyncSet or Config still requests its GVK. Removing it from only one source does not stop replication. See [replication with SyncSets](sync.md#replicating-data-with-syncsets-recommended).

---

## Mutation CRDs (`mutations.gatekeeper.sh`)

`Assign`, `AssignMetadata`, and `ModifySet` use `v1` (also served: `v1alpha1`, `v1beta1`). `AssignImage` uses `v1alpha1`.

Common mutation fields:

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.applyTo[]` | []object | Required for `Assign`, `AssignImage`, and `ModifySet`; not supported by `AssignMetadata`. Each entry needs `groups`, `versions`, and `kinds`; globs are not allowed |
| `spec.applyTo[].operations` | []string | Optional: `CREATE`, `UPDATE`, or `*`. Omitted/empty means all supported mutation operations (currently `CREATE` and `UPDATE`); `*` must appear alone. See [operation filtering](mutation.md#operations-field) |
| `spec.match` | object | Optional [match criteria](mutation.md#extent-of-changes); empty/undefined criteria match everything |
| `spec.location` | string | Required object path, for example `spec.containers[name: main].image`; see [path syntax](mutation.md#intent) |
| `spec.parameters` | object | Mutator-specific options |
| `spec.parameters.pathTests[]` | []object | Optional `subPath` + `condition` (`MustExist` / `MustNotExist`) checks; each subpath must be a prefix of `location`. Supported by `Assign`, `AssignImage`, and `ModifySet`, not `AssignMetadata`; see [conditionals](mutation.md#conditionals) |

Omitted operations and `*` also include any mutation operations supported by future releases. List explicit operations to keep the same scope across upgrades. Operation-filtered mutators only run without admission-operation context (for example during expansion) if they include every supported operation.

### Assign (`v1`)

`spec.parameters.assign` must select exactly one of these value sources:

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.parameters.assign.value` | any | Constant value to assign; may be a scalar, list, or object |
| `spec.parameters.assign.fromMetadata.field` | string | `name` or `namespace` from the object being mutated; see [metadata assignment](mutation.md#assigning-values-from-metadata) |
| `spec.parameters.assign.externalData` | object | External provider configuration; see the [external-data mutation API](externaldata.md#api) for provider selection, data sources, and failure policies |

### AssignMetadata (`v1`)

Adds only missing `metadata.labels` / `metadata.annotations` entries. Existing labels and annotations are not overwritten. `applyTo` and `pathTests` are not supported.

| Field | Description |
| ----- | ----------- |
| `spec.parameters.assign` | Exactly one of the [Assign value sources](#assign-v1); `value` must be a string. External data only supports `dataSource: Username` |

See [AssignMetadata](mutation.md#assignmetadata) for examples.

### AssignImage (`v1alpha1`)

| Field | Description |
| ----- | ----------- |
| `spec.parameters.assignDomain` | Image registry domain (no trailing slash) |
| `spec.parameters.assignPath` | Image path/repository component |
| `spec.parameters.assignTag` | Tag or digest; must start with `:` or `@` |

At least one image component is required. If `assignPath` could be interpreted as a domain, `assignDomain` must also be specified. See [AssignImage](mutation.md#assignimage).

### ModifySet (`v1`)

| Field | Description |
| ----- | ----------- |
| `spec.parameters.operation` | `merge` (default) or `prune` |
| `spec.parameters.values.fromList` | List values to merge into or prune from the location |

See [ModifySet](mutation.md#modifyset).

---

## ExpansionTemplate (`expansion.gatekeeper.sh/v1beta1`)

Also served: `v1alpha1`.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.applyTo[]` | []object | Generator resource GVKs to expand (for example Deployment, Job) |
| `spec.templateSource` | string | Field on the generator used as the pod/template source (often `spec.template`) |
| `spec.generatedGVK` | object | `{group, version, kind}` of the generated resource (for example Pod) |
| `spec.enforcementAction` | string | Optional override for enforcement on expanded resources; empty defers to the constraint |

See [ExpansionTemplate behavior](expansion.md#expansiontemplate-explained).

---

## Connection (`connection.gatekeeper.sh/v1alpha1`)

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.driver` | string | Required export driver name: `dapr` or `disk` |
| `spec.config` | object | Required driver-specific configuration (preserved unknown fields); see [export configuration](export.md#setting-up-audit-to-export-violations) |

Create the Connection in Gatekeeper's namespace and select its name with `--audit-connection`. See [Export](export.md) for audit export configuration and enablement.

---

## Provider (`externaldata.gatekeeper.sh`)

Preferred version: `v1beta1` (also served: `v1alpha1`, deprecated).

Registers an external data provider HTTPS service.

| Field | Type | Description |
| ----- | ---- | ----------- |
| `spec.url` | string | Required provider endpoint URL (must use the `https://` prefix) |
| `spec.timeout` | integer | Request timeout in seconds when querying the provider |
| `spec.caBundle` | string | Required base64-encoded TLS CA bundle in PEM format; see [provider TLS configuration](externaldata.md#how-gatekeeper-trusts-the-external-data-provider-tls) |

See the [Provider API](externaldata.md#provider) for examples.

---

## Status CRDs (`status.gatekeeper.sh`)

Gatekeeper writes these namespaced resources so each controller pod can report health and errors. They are controller-owned operational data, not resources that users normally create or edit.

| Kind | API version |
| ---- | ----------- |
| `ConfigPodStatus` | `status.gatekeeper.sh/v1beta1` |
| `ConnectionPodStatus` | `status.gatekeeper.sh/v1alpha1` |
| `ConstraintPodStatus` | `status.gatekeeper.sh/v1beta1` |
| `ConstraintTemplatePodStatus` | `status.gatekeeper.sh/v1beta1` |
| `ExpansionTemplatePodStatus` | `status.gatekeeper.sh/v1beta1` |
| `MutatorPodStatus` | `status.gatekeeper.sh/v1beta1` |
| `ProviderPodStatus` | `status.gatekeeper.sh/v1beta1` |

---

## Viewing schemas and examples

These links use the Gatekeeper v3.23.1 release rather than the development branch.

* CRD YAML with full OpenAPI schemas: [`config/crd/bases`](https://github.com/open-policy-agent/gatekeeper/tree/v3.23.1/config/crd/bases)
* Helm-packaged CRDs, including Provider: [`charts/gatekeeper/crds`](https://github.com/open-policy-agent/gatekeeper/tree/v3.23.1/charts/gatekeeper/crds)
* End-to-end samples: [Examples](examples.md) and the [demo manifests](https://github.com/open-policy-agent/gatekeeper/tree/v3.23.1/demo)
