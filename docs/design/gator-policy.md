# Gator Policy Design and Implementation

**Status**: MVP Complete, Post-MVP Proposed
**Author**: Sertac Ozercan
**Created**: 2026-01-08
**Last Updated**: 2026-01-08

## Table of Contents

- [Overview](#overview)
- [Mental Model](#mental-model)
- [Part 1: Design](#part-1-design)
  - [MVP Design](#mvp-design)
  - [Known Limitations & Deferred Items](#known-limitations--deferred-items)
  - [Post-MVP Design](#post-mvp-design)
- [Part 2: Implementation](#part-2-implementation)
  - [MVP Implementation](#mvp-implementation)
  - [Post-MVP Implementation](#post-mvp-implementation)

---

# Overview

The `gator policy` command provides a brew-inspired interface for managing Gatekeeper policies from the official [gatekeeper-library](https://github.com/open-policy-agent/gatekeeper-library). It enables users to discover, install, upgrade, and manage ConstraintTemplates and Constraints in their Kubernetes clusters.

---

# Mental Model

This section defines core terminology used throughout this document:

- **Policy**: A single ConstraintTemplate definition. When you install a "policy" (without `--bundle`), you get only the template—no Constraint instances are created. The user must create their own Constraint CRs to activate enforcement.
- **Bundle**: A curated collection of policies **plus** pre-configured Constraint instances. Bundles are opinionated, ready-to-use policy sets (e.g., `pod-security-baseline`). Installing a bundle creates both ConstraintTemplates and their associated Constraints.
- **Catalog**: A YAML index file that describes available policies and bundles. It contains metadata (names, versions, descriptions) and **paths** pointing to the actual template/constraint YAML files in the library repository. The catalog itself contains no policy logic—only references.

> **Key distinction**: Policies give you building blocks; bundles give you turnkey enforcement.

---

# Part 1: Design

## MVP Design

### Command Structure

```
gator policy
├── search <query>              # Search available policies (required query)
├── list                        # List installed policies
├── install <policy...>         # Install one or more policies
│   --bundle <name>             # Install a policy bundle
│   --enforcement-action <act>  # Override enforcement action
│   --dry-run                   # Preview only
├── uninstall <policy...>       # Remove policies
│   --dry-run                   # Preview only
├── update                      # Refresh policy catalog
├── upgrade [policy...]         # Upgrade installed policies
│   --all                       # Upgrade all policies
│   --dry-run                   # Preview only
└── generate-catalog            # Generate catalog from gatekeeper-library
    --library-path <path>       # Path to library repository (default: ".")
    --output, -o <file>         # Output file path (default: "catalog.yaml")
    --name <name>               # Catalog name (default: "gatekeeper-library")
    --version <ver>             # Catalog version (default: "v1.0.0")
  --base-url <url>            # Convert local template/constraint paths to URLs
    --bundles <file>            # Bundles definition file (optional)
    --validate                  # Validate generated catalog (default: true)
```

### MVP Commands

| Command | Description | Requires Cluster |
|---------|-------------|------------------|
| `search` | Search available policies | No (uses cached catalog) |
| `list` | List installed policies | Yes |
| `install` | Install policies/bundles | Yes |
| `uninstall` | Remove policies | Yes |
| `update` | Refresh catalog | No |
| `upgrade` | Upgrade policies | Yes |
| `generate-catalog` | Generate catalog from library | No |

### Installation Model

#### ConstraintTemplates vs Constraints

- **Individual policy install**: Installs **ConstraintTemplate only**
- **Bundle install**: Installs ConstraintTemplates **AND** pre-configured Constraints

```bash
# Template only
gator policy install k8srequiredlabels

# Bundle includes templates + constraints
gator policy install --bundle pod-security-baseline
```

#### Bundles

Bundles are curated sets of policies with pre-configured constraints. They are opinionated and ready-to-use.

| Bundle | Description |
|--------|-------------|
| `pod-security-baseline` | Pod Security Standards - Baseline level |
| `pod-security-restricted` | Pod Security Standards - Restricted level |

### Policy Catalog Schema

The policy catalog is a YAML file that provides metadata about available policies.

#### Catalog Location

- **Production**: `https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/catalog.yaml`
- **Local/Testing**: File path via `GATOR_CATALOG_URL=file:///path/to/catalog.yaml`

#### Schema Definition

```yaml
apiVersion: gator.gatekeeper.sh/v1alpha1
kind: PolicyCatalog
metadata:
  name: gatekeeper-library
  version: v1.0.0
  updatedAt: "2026-01-08T00:00:00Z"
  repository: https://github.com/open-policy-agent/gatekeeper-library

bundles:
  - name: pod-security-baseline
    description: "Enforces Pod Security Standards at Baseline level"
    policies:
      - k8spspprivilegedcontainer
      - k8spspallowprivilegeescalation
      # ... more policies

  - name: pod-security-restricted
    description: "Enforces Pod Security Standards at Restricted level"
    inherits: pod-security-baseline
    policies:
      - k8spsprunasnonroot
      - k8spspseccomp

policies:
  - name: k8srequiredlabels
    version: v1.2.0
    description: "Requires specified labels on all resources"
    category: general
    templatePath: library/general/requiredlabels/template.yaml
    sampleConstraintPath: library/general/requiredlabels/samples/all-must-have-owner/constraint.yaml
    documentationUrl: https://open-policy-agent.github.io/gatekeeper-library/website/requiredlabels

  - name: k8spspprivilegedcontainer
    version: v1.0.0
    description: "Blocks privileged containers"
    category: pod-security
    templatePath: library/pod-security-policy/privileged-containers/template.yaml
    bundleConstraints:
      pod-security-baseline: library/pod-security-policy/privileged-containers/samples/psp-privileged-container/constraint.yaml
      pod-security-restricted: library/pod-security-policy/privileged-containers/samples/psp-privileged-container/constraint.yaml
    bundles:
      - pod-security-baseline
      - pod-security-restricted
```

#### Field Descriptions

##### Metadata

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Catalog identifier |
| `version` | string | Yes | Catalog schema version (semver) |
| `updatedAt` | string | Yes | ISO 8601 timestamp |
| `repository` | string | Yes | Source repository URL |

##### Bundle

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Bundle identifier |
| `description` | string | Yes | Human-readable description |
| `inherits` | string | No | Parent bundle to inherit policies from |
| `policies` | []string | Yes | List of policy names in this bundle |

##### Policy

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Policy identifier |
| `version` | string | Yes | Policy version (semver) |
| `description` | string | Yes | Human-readable description |
| `category` | string | Yes | Category: `general`, `pod-security`, etc. |
| `templatePath` | string | Yes | Relative path to ConstraintTemplate YAML |
| `bundleConstraints` | map[string]string | No | Maps bundle names to their constraint YAML paths. Different bundles may need different constraint configurations for the same template (e.g., baseline vs restricted PSS profiles). |
| `sampleConstraintPath` | string | No | Relative path to sample Constraint |
| `documentationUrl` | string | No | Link to documentation |
| `bundles` | []string | No | Bundle(s) this policy belongs to |

#### Path Resolution Rules

The `templatePath`, `bundleConstraints`, and `sampleConstraintPath` fields are **always relative to the library repository root**, regardless of catalog location:

| Catalog Source | Path Resolution |
|----------------|----------------|
| `https://...` URL | Paths are appended to the repository's raw content URL. E.g., `templatePath: library/general/requiredlabels/template.yaml` resolves to `https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/library/general/requiredlabels/template.yaml` |
| `file:///path/to/catalog.yaml` | Paths are resolved relative to the **parent directory** of the catalog file (assumed to be the library root). E.g., if catalog is at `/repos/gatekeeper-library/catalog.yaml`, then `library/general/...` resolves to `/repos/gatekeeper-library/library/general/...` |

> **Important**: Paths must not start with `/` or `./`. They are always library-root-relative.

### Versioning

#### Version Terminology

| Term | Field | Description | Format |
|------|-------|-------------|--------|
| **Catalog Schema Version** | `metadata.version` | Format/contract version of the catalog structure itself. Increment when adding/removing fields. | semver (e.g., `v1.0.0`) |
| **Catalog Content Version** | `metadata.updatedAt` | Timestamp indicating when this catalog snapshot was generated from the library. | ISO 8601 timestamp |
| **Policy Version** | `policies[].version` | Version of the individual ConstraintTemplate/Constraint. Tracks policy logic changes. | semver (e.g., `v1.2.0`) |

#### Version Behavior

Policies are always installed at the version currently available in the catalog. There is no support for installing older versions of policies.

> **Limitation**: The gatekeeper-library does not maintain historical versions of policy templates. Each policy has exactly one version at any given time. When the library updates a policy, the previous version is replaced. As a result, `gator policy install` always installs the current version from the catalog.

If you need a specific older version of a policy, you must:
1. Check out the gatekeeper-library at the desired git tag/commit
2. Use `gator policy generate-catalog` to create a catalog from that version
3. Configure `GATOR_CATALOG_URL` to point to your custom catalog

### Configuration

#### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GATOR_HOME` | Cache directory | `~/.config/gator/` |
| `GATOR_CATALOG_URL` | Catalog source URL | `https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/catalog.yaml` |
| `KUBECONFIG` | Kubernetes config | `~/.kube/config` |

#### Cache Structure

```
$GATOR_HOME/              # Default: ~/.config/gator/
├── catalog.yaml          # Cached catalog
└── config.yaml           # User preferences (future)
```

#### Cache Behavior Rules

| Scenario | Behavior |
|----------|----------|
| **First run, no cache** | `search`, `install`, `upgrade` will **error** with a message to run `update` first. Only `update` and `generate-catalog` work without a cache. |
| **Staleness / TTL** | No automatic refresh. The cache is used until explicitly replaced by `update`. Users control when to refresh. |
| **`update` behavior** | Always fetches from `GATOR_CATALOG_URL` and **overwrites** the cached catalog on success. |
| **`update` failure** | If fetch fails, the **existing cache is preserved** (last-known-good). An error is returned but the cache remains usable. |
| **Cache corruption** | If the cached file is invalid YAML, commands will error. Run `update` to replace it. |

### Resource Management

#### Labels and Annotations

All managed resources receive these metadata:

```yaml
metadata:
  labels:
    gatekeeper.sh/managed-by: gator
    gatekeeper.sh/bundle: pod-security-baseline  # If installed via bundle
  annotations:
    gatekeeper.sh/policy-version: v1.0.0
    gatekeeper.sh/policy-source: gatekeeper-library
    gatekeeper.sh/installed-at: "2026-01-08T10:30:00Z"
```

#### Conflict Resolution

| Scenario | Behavior |
|----------|----------|
| Resource doesn't exist | Install it |
| Resource exists, managed by gator | Update if version differs |
| Resource exists, NOT managed by gator | **Error** - refuse to modify |
| Resource exists, same version | Skip (already installed) |

### Error Handling

#### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (network, invalid args) |
| 2 | Cluster error (not found, permission denied) |
| 3 | Conflict (resource exists, not managed by gator) |
| 4 | Partial success (some resources failed) |

### Command Examples

#### Search

```bash
$ gator policy search labels

NAME                  VERSION   CATEGORY   DESCRIPTION
k8srequiredlabels     v1.2.0    general    Requires specified labels on resources
```

#### Install

```bash
# Single policy
$ gator policy install k8srequiredlabels
✓ k8srequiredlabels (v1.2.0) installed

# Bundle with enforcement override
$ gator policy install --bundle pod-security-baseline --enforcement-action=warn
Installing pod-security-baseline bundle (8 policies)...
✓ k8spspprivilegedcontainer (v1.0.0)
...
✓ Installed 8 templates, 8 constraints
```

#### Update & Upgrade

```bash
$ gator policy update
Fetching catalog from gatekeeper-library...
Updated catalog to version v1.5.0

$ gator policy upgrade --all
✓ k8srequiredlabels upgraded (v1.1.0 → v1.2.0)
```

#### Generate Catalog

```bash
$ gator policy generate-catalog --library-path=/path/to/gatekeeper-library
Scanning library directory...
Found 45 policies across 5 categories.
Catalog written to catalog.yaml
```

### User Experience Principles

1. **No confirmation prompts** - Use `--dry-run` for safety preview
2. **Fail fast** - Stop on first error, allow re-run (idempotent)
3. **Line-by-line progress** - Immediate feedback, CI-friendly
4. **Explicit over implicit** - `--bundle` flag, `--all` for upgrade
5. **Scriptable** - JSON output, meaningful exit codes

### Output Format Options

All commands support `--output` / `-o` flag:

| Format | Flag | Description |
|--------|------|-------------|
| Table | `-o table` (default) | Human-readable table output |
| JSON | `-o json` | Machine-readable JSON output |

**JSON Schema Stability**:
- The JSON output schema is **versioned** and follows the catalog `apiVersion` (e.g., `gator.gatekeeper.sh/v1alpha1`).
- Fields in `v1alpha1` may change without notice. Stability guarantees begin at `v1beta1`.
- JSON output always includes a `apiVersion` field for schema identification.

**Table Column Stability**:
- Columns `NAME`, `VERSION`, `STATUS` are stable across versions.
- Additional columns (e.g., `CATEGORY`, `BUNDLE`) are best-effort and may change.

### Flag Precedence and Mutual Exclusivity

| Scenario | Behavior |
|----------|----------|
| `upgrade --all` vs explicit policies | **Error**: mutually exclusive. Either upgrade all or specify names. |
| `install --bundle X` with positional policies | **Allowed**: installs the bundle AND the additional policies. Bundle policies get both templates and constraints; additional positional policies get **template-only** (no constraints). Duplicates are deduplicated. |
| `install --bundle X --bundle Y` | **Allowed**: both bundles are installed. Duplicate policies are deduplicated. |

### Bundle Inheritance Semantics

When a bundle specifies `inherits: <parent-bundle>`, the following rules apply:

1. **Recursive expansion**: Inheritance chains are resolved recursively (e.g., A inherits B, B inherits C → A gets C + B + A policies).
2. **Order**: Parent policies are processed **before** child policies (depth-first, parent-first).
3. **Deduplication**: If the same policy appears in multiple bundles, it is installed once. The **first occurrence** (from the deepest ancestor) wins for any policy-level settings.
4. **Cycle detection**: Circular inheritance is detected at catalog load time and results in a validation error.
5. **Maximum depth**: Inheritance is limited to 10 levels to prevent runaway expansion.

### Conflict Resolution (Detailed)

**How "managed by gator" is determined**:
- A resource is considered managed if it has **both**:
  - Label: `gatekeeper.sh/managed-by: gator`
  - Annotation: `gatekeeper.sh/policy-source` (any value)

**Conflict scenarios**:

| Resource State | Behavior |
|----------------|----------|
| Does not exist | Create it |
| Exists with gator labels | Update if version differs; skip if same |
| Exists without gator labels | **Conflict error** (exit code 3) |
| Exists with partial gator labels (only one of label/annotation) | **Conflict error** - treat as unmanaged |

**Adopt flow**: MVP does **not** support adopting unmanaged resources. Post-MVP may add `--adopt` flag to take ownership of existing resources by adding gator labels.

### Enforcement Action Override Behavior

| Scenario | `--enforcement-action` Behavior |
|----------|----------------------------------|
| **Template-only install** (no `--bundle`) | Flag is **ignored with a warning**. Templates don't have enforcement actions; only Constraints do. |
| **Bundle install** (new constraints) | Flag value is applied to all newly created Constraints. |
| **Bundle install** (existing managed constraints) | Existing constraints are **updated** to the new enforcement action. |
| **Upgrade** (managed constraints) | Enforcement action is **preserved** from the installed constraint. To change it, use `--enforcement-action` explicitly. |
| **Upgrade with `--enforcement-action`** | Overrides the enforcement action on all upgraded constraints. |

### Error Handling (Detailed)

**"Fail fast" vs "Partial success" clarification**:

- **Default behavior (MVP)**: Fail fast. On the first error, stop processing and return the appropriate exit code.
- **Exit code 4 (partial success)** occurs only with `--continue-on-error` flag (if implemented) or in specific batch operations where some resources succeed before a failure.
- For MVP, most operations use fail-fast. Exit code 4 is reserved for future batch modes.

| Exit Code | Condition |
|-----------|-----------|
| 0 | All operations succeeded |
| 1 | General error (invalid args, network failure, invalid catalog) |
| 2 | Cluster error (unreachable, permission denied, Gatekeeper not installed) |
| 3 | Conflict (resource exists but not managed by gator) |
| 4 | Partial success (some operations succeeded, some failed) - future use |

### Security and Integrity (MVP)

**Catalog Trust Model**:
- **HTTPS required**: By default, only `https://` catalog URLs are accepted.
- **`file://` allowed**: For local development/testing only. Not recommended for production.
- **Plain HTTP rejected**: `http://` URLs are rejected with an error. Use `--insecure` flag to override (with warning).

**Path Traversal Protection**:
- All content paths (e.g., `templatePath`, `constraintPath` in the catalog) are validated.
- Paths containing `..` sequences are rejected to prevent directory traversal attacks.
- Both URL paths (for HTTP fetches) and file paths (for `file://` fetches) are validated.

**Resource Validation**:
- Before applying any resource, gator validates:
  - `apiVersion` matches expected Gatekeeper API groups (`templates.gatekeeper.sh`, `constraints.gatekeeper.sh`)
  - `kind` is `ConstraintTemplate` or a valid Constraint kind
  - YAML is well-formed and passes basic structural validation
- Invalid resources are rejected before any cluster modification.

**Sensitive Data**:
- `--dry-run` output **redacts** any fields matching common secret patterns (e.g., `password`, `token`, `secret`).
- Logs never include full resource specs at INFO level; use DEBUG for full output.

### Performance Considerations

**API Call Expectations**:

| Operation | Approximate API Calls |
|-----------|----------------------|
| `list` | 1 (list ConstraintTemplates with label selector) |
| `install` (1 policy) | 2-3 (get, create/update template, optionally constraint) |
| `install --bundle` (N policies) | 2N + overhead (batched where possible) |
| `upgrade --all` (N policies) | N+1 (list + update each) |

**Batching Strategy**:
- Multiple resources are applied sequentially (not in parallel) to ensure deterministic ordering and clear error attribution.
- Server-side apply is used where supported to minimize round-trips.
- Future optimization: batch multiple ConstraintTemplates in a single apply for large bundles.

**Latency Targets**:
- `search` / `list`: < 500ms (local cache / single API call)
- `install` (single policy): < 2s
- `install --bundle` (10 policies): < 10s

### Known Limitations & Deferred Items

The following capabilities were identified during the MVP review process and are **intentionally deferred** to post-MVP. They are documented here so they are not lost and can be prioritized in future iterations.

#### 1. `scoped` Enforcement Action (Install & Upgrade)

The `scoped` enforcement action (which delegates enforcement decisions to individual constraints) is **not supported** in the MVP. Both `install --enforcement-action` and `upgrade --enforcement-action` reject `scoped` with an error.

**Rationale**: `scoped` requires additional per-constraint configuration that does not fit the current simplified install model. Supporting it properly requires a way to specify per-constraint enforcement scopes, likely through the declarative `PolicyFile` approach (see Post-MVP).

**Workaround**: Users who need `scoped` enforcement can manually edit the Constraint CR after installation.

#### 2. `--exclude-policies` for `generate-catalog`

The `generate-catalog` command currently includes **all** discovered policies from the library. There is no mechanism to exclude specific policies or categories during catalog generation.

**Rationale**: The initial catalog generation is expected to be comprehensive. Selective inclusion/exclusion adds complexity without clear MVP use cases.

**Future**: Add `--exclude-policies` and/or `--include-categories` flags to `generate-catalog`.

#### 3. `--all` Flag for `uninstall`

The `uninstall` command requires explicit policy names or `--bundle`. There is no `--all` flag to uninstall every gator-managed resource in one command.

**Rationale**: Mass uninstall is a destructive operation. Requiring explicit names or bundles reduces the risk of accidental removal of all policies from a cluster.

**Future**: Add `--all` flag with a confirmation prompt or require `--yes` for non-interactive use.

#### 4. Multiple Catalog Sources

The MVP supports a single catalog source configured via `GATOR_CATALOG_URL`. There is no support for merging policies from multiple catalogs or custom registries.

**Rationale**: The MVP focuses on the official `gatekeeper-library` as the sole source. Multi-source support requires a merge strategy, conflict resolution rules, and priority ordering.

**Future**: Support multiple catalog URLs (e.g., via a config file) with precedence rules. This also relates to the Post-MVP OCI catalog support.

#### 5. Mutex / File Locking for Concurrent Processes

The local catalog cache (`$GATOR_HOME/catalog.yaml`) has no file locking. Concurrent `gator policy update` or `gator policy install` processes running against the same cache directory may race on reads/writes.

**Rationale**: CLI tools are typically invoked sequentially. Concurrent invocation is uncommon in practice and adds implementation complexity.

**Future**: Add advisory file locking (e.g., `flock`) around cache read/write operations, or use atomic rename for writes.

#### 6. Mutation Policy Support in Catalog Generator

The `generate-catalog` command discovers only **validation** policies (ConstraintTemplates). Mutation policies (`Assign`, `AssignMetadata`, `ModifySet`) in the `mutation/` directory of the gatekeeper-library are not included.

**Rationale**: The MVP policy management model is built around ConstraintTemplates and Constraints. Mutation resources have a different lifecycle and configuration model.

**Future**: Extend the catalog schema and generator to support mutation policies as a separate resource type.

#### 7. Cross-Cluster and Multi-Cluster Support

The MVP client operates against a single cluster determined by the current `KUBECONFIG` context. There is no built-in support for managing policies across multiple clusters.

**Rationale**: Multi-cluster management significantly increases scope and requires decisions around consistency models, rollout strategies, and failure handling.

**Future**: The PolicySync controller (see Post-MVP) provides a foundation for multi-cluster management. Additionally, a `--context` flag could allow targeting specific kubeconfig contexts.

---

## Post-MVP Design

### Post-MVP CLI Commands

| Command | Description |
|---------|-------------|
| `info <policy>` | Show detailed policy information |
| `diff <policy>` | Show differences between installed and latest |
| `apply -f <file>` | Declarative policy management |
| `doctor` | Diagnose issues |

#### Declarative Configuration

```yaml
# gator.yaml
apiVersion: gator.gatekeeper.sh/v1alpha1
kind: PolicyFile
metadata:
  name: my-cluster-policies
spec:
  bundles:
    - name: pod-security-baseline
      enforcementAction: warn
  policies:
    - name: k8srequiredlabels
      version: ">=1.0.0"
```

```bash
gator policy apply -f gator.yaml
gator policy apply -f gator.yaml --prune  # Remove unlisted
```

#### Info Command

```bash
$ gator policy info k8srequiredlabels

Name:        k8srequiredlabels
Version:     v1.2.0
Category:    general
Description: Requires specified labels on resources

Parameters:
  - labels (array, required): List of required label keys
  - message (string, optional): Custom violation message
```

#### Doctor Command

```bash
$ gator policy doctor

✓ Catalog reachable (v1.5.0)
✓ Cluster accessible (gatekeeper v3.15.0)
✓ 12 policies installed

All checks passed.
```

### PolicySync Controller (GitOps Integration)

The PolicySync controller enables GitOps-style policy management with automatic drift detection and remediation.

#### Motivation

- **Manual synchronization**: Currently, users must manually run `gator policy install/upgrade` commands
- **Drift detection**: No mechanism to detect or remediate unauthorized policy changes
- **Multi-cluster management**: Difficult to ensure policy consistency across many clusters
- **Version tracking**: No centralized view of which policy versions are deployed

#### Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                               │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐ │
│  │               Gator Sync Controller (Deployment)                    │ │
│  │                                                                     │ │
│  │  ┌───────────────┐  ┌────────────────┐  ┌───────────────────────┐  │ │
│  │  │   Catalog     │  │  PolicySync    │  │   Status/Metrics      │  │ │
│  │  │   Fetcher     │──│  Reconciler    │──│   Reporter            │  │ │
│  │  └───────────────┘  └────────────────┘  └───────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
│  ┌───────────────────┐  ┌─────────────────────────────────────────────┐ │
│  │  PolicySync CRD   │  │  Managed Resources                          │ │
│  │  (User Config)    │  │  - ConstraintTemplates (owner: PolicySync)  │ │
│  │                   │  │  - Constraints (owner: PolicySync)          │ │
│  └───────────────────┘  └─────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
                │
                ▼ HTTPS
        ┌───────────────────┐
        │   Policy Catalog  │
        └───────────────────┘
```

#### PolicySync CRD

```yaml
apiVersion: gator.gatekeeper.sh/v1alpha1
kind: PolicySync
metadata:
  name: production-policies
spec:
  catalog:
    url: "https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/catalog.yaml"

  syncInterval: 5m

  policies:
    include:
      - k8srequiredlabels
      - k8scontainerlimits
    exclude:
      - k8sdisallowedtags
    bundles:
      - security-essentials

  constraintDefaults:
    enforcementAction: warn
    match:
      excludedNamespaces:
        - kube-system
        - gatekeeper-system

  policyOverrides:
    - name: k8srequiredlabels
      enforcementAction: deny
      parameters:
        labels:
          - key: owner
            allowedRegex: "^team-.+$"

  drift:
    enabled: true
    action: restore  # restore | warn | ignore
    checkInterval: 1m

  suspended: false

status:
  phase: Synced
  catalog:
    version: "v2026.01.08"
    lastFetched: "2026-01-08T04:00:00Z"
  summary:
    desired: 15
    synced: 14
    failed: 1
  conditions:
    - type: Ready
      status: "True"
      reason: SyncComplete
```

#### Sync Controller CLI Commands

```bash
# Start the sync controller
gator policy sync-controller [flags]
  --kubeconfig string       Path to kubeconfig
  --metrics-addr string     Address for metrics endpoint (default ":8080")
  --sync-concurrency int    Number of concurrent sync operations (default 5)

# One-shot sync (for CI/CD)
gator policy sync [flags]
  --catalog-url string      Catalog URL
  --policies strings        Policies to sync
  --bundles strings         Bundles to sync
  --dry-run                 Preview changes

# View sync status
gator policy status [policysync-name]

# Trigger immediate sync
gator policy trigger-sync [policysync-name]
```

#### Observability

##### Metrics

```prometheus
gator_policy_sync_operations_total{policysync="...", status="success|failure"}
gator_policy_sync_policies_total{policysync="...", phase="synced|failed|pending"}
gator_policy_sync_drift_detected_total{policysync="...", policy="...", action="restore|warn"}
```

##### Events

```
Normal  SyncStarted      Syncing 15 policies from catalog v2026.01.08
Normal  PolicySynced     Successfully synced policy k8srequiredlabels (v1.0.2)
Warning DriftDetected    Drift detected in ConstraintTemplate k8srequiredlabels
Normal  DriftRestored    Restored ConstraintTemplate k8srequiredlabels to desired state
```

---

# Part 2: Implementation

## MVP Implementation

### Package Structure

```
cmd/gator/
├── main.go                      # Existing entry point
└── policy/                      # Policy command package
    ├── policy.go                # Root "policy" command
    ├── search.go                # search subcommand
    ├── list.go                  # list subcommand
    ├── install.go               # install subcommand
    ├── uninstall.go             # uninstall subcommand
    ├── update.go                # update subcommand
    ├── upgrade.go               # upgrade subcommand
    └── generate_catalog.go      # generate-catalog subcommand

pkg/gator/policy/                # Policy business logic
├── catalog/
│   ├── types.go                 # Catalog types (PolicyCatalog, Policy, Bundle)
│   ├── fetch.go                 # HTTP/file fetching and catalog loading
│   ├── fetch_test.go            # Tests for fetching
│   ├── cache.go                 # Local cache management
│   ├── generator.go             # Catalog generation from library
│   ├── generator_test.go        # Tests for generator
│   └── catalog_test.go          # Tests for catalog parsing
├── client/
│   ├── client.go                # Kubernetes client wrapper and list/query operations
│   ├── client_test.go           # Tests for client
│   ├── install.go               # Install operations
│   ├── uninstall.go             # Uninstall operations
│   └── upgrade.go               # Upgrade operations
├── labels/
│   ├── labels.go                # Label/annotation constants and helpers
│   └── labels_test.go           # Tests for labels
├── exitcodes.go                 # Exit code constants
├── exitcodes_test.go            # Tests for exit codes
└── output/
    ├── output.go                # Output formatting interface
    ├── output_test.go           # Tests for output
    ├── table.go                 # Table output (human-readable)
    └── json.go                  # JSON output
```

### Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework (already used by gator) |
| `k8s.io/client-go` | Kubernetes client (already used) |
| `sigs.k8s.io/yaml` | YAML parsing (already used) |

No new dependencies required.

### Core Types

#### Catalog Types

```go
// pkg/gator/policy/catalog/types.go

package catalog

import "time"

type PolicyCatalog struct {
    APIVersion string          `json:"apiVersion" yaml:"apiVersion"`
    Kind       string          `json:"kind" yaml:"kind"`
    Metadata   CatalogMetadata `json:"metadata" yaml:"metadata"`
    Bundles    []Bundle        `json:"bundles" yaml:"bundles"`
    Policies   []Policy        `json:"policies" yaml:"policies"`
}

type CatalogMetadata struct {
    Name       string    `json:"name" yaml:"name"`
    Version    string    `json:"version" yaml:"version"`
    UpdatedAt  time.Time `json:"updatedAt" yaml:"updatedAt"`
    Repository string    `json:"repository" yaml:"repository"`
}

type Bundle struct {
    Name        string   `json:"name" yaml:"name"`
    Description string   `json:"description" yaml:"description"`
    Inherits    string   `json:"inherits,omitempty" yaml:"inherits,omitempty"`
    Policies    []string `json:"policies" yaml:"policies"`
}

type Policy struct {
    Name                 string   `json:"name" yaml:"name"`
    Version              string   `json:"version" yaml:"version"`
    Description          string   `json:"description" yaml:"description"`
    Category             string   `json:"category" yaml:"category"`
    TemplatePath         string   `json:"templatePath" yaml:"templatePath"`
    ConstraintPath       string   `json:"constraintPath,omitempty" yaml:"constraintPath,omitempty"`
    SampleConstraintPath string   `json:"sampleConstraintPath,omitempty" yaml:"sampleConstraintPath,omitempty"`
    DocumentationURL     string   `json:"documentationUrl,omitempty" yaml:"documentationUrl,omitempty"`
    Bundles              []string `json:"bundles,omitempty" yaml:"bundles,omitempty"`
}
```

#### Fetcher Interface

```go
// pkg/gator/policy/catalog/fetch.go

const (
    DefaultCatalogURL = "https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/catalog.yaml"
    DefaultTimeout    = 30 * time.Second
)

type Fetcher interface {
    Fetch(ctx context.Context, catalogURL string) ([]byte, error)
    FetchContent(ctx context.Context, contentURL string) ([]byte, error)
}

type HTTPFetcher struct {
    client  *http.Client
    baseURL string
    mu      sync.RWMutex // protects baseURL for thread-safety
}

func NewHTTPFetcher(timeout time.Duration) *HTTPFetcher {
    return &HTTPFetcher{
        client: &http.Client{Timeout: timeout},
    }
}
```

#### Cache Management

```go
// pkg/gator/policy/catalog/cache.go

type Cache struct {
    dir string
}

func NewCache() (*Cache, error) {
    dir := os.Getenv("GATOR_HOME")
    if dir == "" {
        home, _ := os.UserHomeDir()
        dir = filepath.Join(home, ".config", "gator")
    }
    os.MkdirAll(dir, 0755)
    return &Cache{dir: dir}, nil
}

func (c *Cache) CatalogPath() string {
    return filepath.Join(c.dir, "catalog.yaml")
}

func (c *Cache) SaveCatalog(data []byte) error {
    return os.WriteFile(c.CatalogPath(), data, 0644)
}

func (c *Cache) LoadCatalog() ([]byte, error) {
    return os.ReadFile(c.CatalogPath())
}
```

#### Kubernetes Client

```go
// pkg/gator/policy/client/client.go

type Client interface {
    GatekeeperInstalled(ctx context.Context) (bool, error)
    ListManagedTemplates(ctx context.Context) ([]InstalledPolicy, error)
    GetTemplate(ctx context.Context, name string) (*unstructured.Unstructured, error)
    InstallTemplate(ctx context.Context, template *unstructured.Unstructured) error
    InstallConstraint(ctx context.Context, constraint *unstructured.Unstructured) error
    DeleteTemplate(ctx context.Context, name string) error
}

type InstalledPolicy struct {
    Name        string
    Version     string
    Bundle      string
    InstalledAt string
    ManagedBy   string
}
```

#### Labels Helper

```go
// pkg/gator/policy/labels/labels.go

const (
    LabelManagedBy        = "gatekeeper.sh/managed-by"
    LabelBundle           = "gatekeeper.sh/bundle"
    AnnotationVersion     = "gatekeeper.sh/policy-version"
    AnnotationSource      = "gatekeeper.sh/policy-source"
    AnnotationInstalledAt = "gatekeeper.sh/installed-at"
    ManagedByValue        = "gator"
    SourceValue           = "gatekeeper-library"
)

func AddManagedLabels(obj *unstructured.Unstructured, version, bundle string)
func IsManagedByGator(obj *unstructured.Unstructured) bool
func GetPolicyVersion(obj *unstructured.Unstructured) string
func GetBundle(obj *unstructured.Unstructured) string
func GetInstalledAt(obj *unstructured.Unstructured) string
```

#### Exit Codes

```go
// pkg/gator/policy/exitcodes.go

const (
    ExitSuccess        = 0
    ExitGeneralError   = 1
    ExitClusterError   = 2
    ExitConflictError  = 3
    ExitPartialSuccess = 4
)

type ExitError struct {
    Code    int
    Message string
}

func (e *ExitError) Error() string { return e.Message }

func NewExitError(code int, message string) *ExitError
func NewGeneralError(message string) *ExitError
func NewClusterError(message string) *ExitError
func NewConflictError(message string) *ExitError
func NewPartialSuccessError(message string) *ExitError
```

### CLI Structure

```go
// cmd/gator/policy/policy.go

var Cmd = &cobra.Command{
    Use:     "policy",
    Short:   "Manage Gatekeeper policies from the policy library",
    Long:    "Install, upgrade, and manage Gatekeeper policies from the official gatekeeper-library.",
    Example: examples,
}

func init() {
    Cmd.AddCommand(
        newSearchCommand(),
        newListCommand(),
        newInstallCommand(),
        newUninstallCommand(),
        newUpdateCommand(),
        newUpgradeCommand(),
        newGenerateCatalogCommand(),
    )
}
```

### Testing Strategy

#### Local Testing

```bash
# Set catalog to local file
export GATOR_CATALOG_URL=file://$(pwd)/test/gator/policy/testdata/catalog.yaml

# Test commands
gator policy update
gator policy search test
gator policy install test-policy-1 --dry-run
```

#### CI Testing

```yaml
- name: Test gator policy
  run: |
    make gator
    export GATOR_CATALOG_URL=file://${{ github.workspace }}/test/gator/policy/testdata/catalog.yaml
    ./bin/gator policy update
    ./bin/gator policy search test
```

#### E2E Testing

```bash
# With kind cluster
kind create cluster
kubectl apply -f deploy/gatekeeper.yaml
export GATOR_CATALOG_URL=file://testdata/catalog.yaml
gator policy install test-policy-1
gator policy list
gator policy uninstall test-policy-1
```

---

## Post-MVP Implementation

### PolicySync Controller Implementation

#### API Types

```go
// pkg/apis/policysync/v1alpha1/types.go

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
type PolicySync struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   PolicySyncSpec   `json:"spec,omitempty"`
    Status PolicySyncStatus `json:"status,omitempty"`
}

type PolicySyncSpec struct {
    Catalog            CatalogSource       `json:"catalog"`
    SyncInterval       metav1.Duration     `json:"syncInterval,omitempty"`
    Policies           PolicySelection     `json:"policies,omitempty"`
    ConstraintDefaults *ConstraintDefaults `json:"constraintDefaults,omitempty"`
    PolicyOverrides    []PolicyOverride    `json:"policyOverrides,omitempty"`
    Drift              *DriftConfig        `json:"drift,omitempty"`
    Suspended          bool                `json:"suspended,omitempty"`
}

type CatalogSource struct {
    URL          string              `json:"url,omitempty"`
    ConfigMapRef *ConfigMapReference `json:"configMapRef,omitempty"`
    OCIRef       string              `json:"ociRef,omitempty"`
}

type PolicySelection struct {
    Include []string `json:"include,omitempty"`
    Exclude []string `json:"exclude,omitempty"`
    Bundles []string `json:"bundles,omitempty"`
}

type DriftConfig struct {
    Enabled       bool            `json:"enabled,omitempty"`
    Action        string          `json:"action,omitempty"`  // restore | warn | ignore
    CheckInterval metav1.Duration `json:"checkInterval,omitempty"`
}

type PolicySyncStatus struct {
    Phase              string             `json:"phase,omitempty"`
    Catalog            *CatalogStatus     `json:"catalog,omitempty"`
    LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`
    Summary            SyncSummary        `json:"summary,omitempty"`
    Policies           []PolicyStatus     `json:"policies,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
}
```

#### Controller Reconciler

```go
type PolicySyncReconciler struct {
    client.Client
    Scheme         *runtime.Scheme
    CatalogFetcher *catalog.Fetcher
    PolicyClient   *policyclient.Client
    Recorder       record.EventRecorder
}

func (r *PolicySyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch PolicySync resource
    // 2. Check if suspended
    // 3. Fetch catalog (with caching)
    // 4. Resolve desired policies (include/exclude/bundles)
    // 5. Get current state (installed policies with owner ref)
    // 6. Calculate diff (add/update/remove)
    // 7. Apply changes with owner references
    // 8. Update status
    // 9. Requeue after syncInterval
}
```

#### RBAC Requirements

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: gator-sync-controller
rules:
  - apiGroups: ["gator.gatekeeper.sh"]
    resources: ["policysyncs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["gator.gatekeeper.sh"]
    resources: ["policysyncs/status"]
    verbs: ["update", "patch"]
  - apiGroups: ["templates.gatekeeper.sh"]
    resources: ["constrainttemplates"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["constraints.gatekeeper.sh"]
    resources: ["*"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["configmaps", "secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
```

#### Helm Chart Integration

```yaml
# values.yaml additions
policySync:
  enabled: false
  image:
    repository: openpolicyagent/gator
    tag: ""
  replicas: 1
  resources:
    limits:
      cpu: 200m
      memory: 256Mi
  defaultPolicySync:
    enabled: false
    catalog:
      url: "https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/catalog.yaml"
    syncInterval: 5m
    constraintDefaults:
      enforcementAction: warn
```

### Future Considerations

1. **OCI Catalog Support**: Fetch catalogs from OCI registries
2. **Signature Verification**: Cosign/Notation support for catalog authenticity
3. **Multi-Cluster**: Federation support for syncing to multiple clusters
4. **Approval Workflow**: Require approval before applying policy changes
5. **Rollback**: Automatic rollback on policy failures
6. **UI Dashboard**: Visual policy sync status
