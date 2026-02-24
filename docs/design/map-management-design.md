# Design Document: MutatingAdmissionPolicy Management Feature

**Issue**: [#4261](https://github.com/open-policy-agent/gatekeeper/issues/4261)  
**Author**: Jaydip Gabani  
**Status**: Draft  
**Created**: January 30, 2026  
**Updated**: February 19, 2026  

---

## Table of Contents

1. [Overview](#overview)
2. [Goals and Non-Goals](#goals-and-non-goals)
3. [Background](#background)
4. [Proposal](#proposal)
5. [API Design](#api-design)
6. [Controller Architecture](#controller-architecture)
7. [Scope Synchronization](#scope-synchronization)
8. [Ownership and Lifecycle](#ownership-and-lifecycle)
9. [Kubernetes Version Compatibility](#kubernetes-version-compatibility)
10. [Security Considerations](#security-considerations)
11. [Known Limitations](#known-limitations)
12. [Open Questions](#open-questions)
13. [Future Work](#future-work)
14. [Alternatives Considered](#alternatives-considered)

---

## Overview

This document describes adding MutatingAdmissionPolicy (MAP) management to Gatekeeper. This feature introduces `MAPTemplate` and `MutationConstraint` CRDs to manage MAP, MutatingAdmissionPolicyBinding (MAPB), and param resources.

---

## Goals and Non-Goals

### Goals

1. Introduce `MAPTemplate` and `MutationConstraint` CRDs for managing MAP resources
2. Embed CEL expressions into Kubernetes MAP spec (with Gatekeeper-injected variable bindings)
3. Sync enforcement scope with Gatekeeper's configuration automatically
4. Use `MutationConstraint` as the param resource
5. Auto-detect Kubernetes API version (alpha/beta/GA)
6. Use owner references for garbage collection
7. Protect against privilege escalation via admission control on MAPTemplate

### Non-Goals

1. Converting existing Gatekeeper mutators (Assign, AssignMetadata, ModifySet, AssignImage) to MAP
2. Replacing existing mutation webhook - these are parallel capabilities
3. Providing fallback if MAP generation fails
4. Extending gator CLI for MAP testing (future work)
5. Migration tooling from existing mutators to MAP

---

## Background

### Kubernetes MutatingAdmissionPolicy (KEP-3962)

| Version | Kubernetes | Status |
|---------|------------|--------|
| Alpha   | v1.32      | Feature gate required |
| Beta    | v1.34      | Enabled by default |
| GA      | v1.36      | Stable |

MAP components:
- **MutatingAdmissionPolicy**: Mutation logic (CEL expressions, match constraints)
- **MutatingAdmissionPolicyBinding**: Binds policies to params and defines scope
- **Param Resources**: Optional CRs providing runtime configuration

---

## Proposal

### Architecture

```
MAPTemplate ────────────► MutatingAdmissionPolicy
     │                          │ (paramKind)
     │ (generates CRD)          ▼
     ▼                    MAPBinding (paramRef)
MutationConstraint ────────────────┘
```

**Resource Count**: User creates 2 (MAPTemplate, MutationConstraint), Gatekeeper generates 3 (CRD, MAP, MAPB).

See [Controller Architecture](#controller-architecture) for the full reconciliation flows,
ownership model, and deletion semantics.

---

## API Design

### Go Type Definitions

#### MAPTemplate Types

**Location**: `apis/maptemplate/v1alpha1/types.go`

```go
// MAPTemplate is the Schema for the MAPTemplate API.
// It defines a reusable mutation policy template that generates a
// MutatingAdmissionPolicy and a CRD for MutationConstraint instances.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
type MAPTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MAPTemplateSpec   `json:"spec,omitempty"`
	Status MAPTemplateStatus `json:"status,omitempty"`
}

// MAPTemplateSpec defines the desired state of MAPTemplate.
type MAPTemplateSpec struct {
	// CRD describes the dynamically generated MutationConstraint CRD.
	CRD MAPTemplateCRD `json:"crd,omitempty"`

	// Policy contains the MutatingAdmissionPolicy spec fields.
	// CEL expressions use Gatekeeper-injected variable bindings
	// (e.g., variables.params.<field>).
	Policy MAPPolicy `json:"policy,omitempty"`
}

// MAPTemplateCRD describes the generated CRD for MutationConstraint instances.
type MAPTemplateCRD struct {
	Spec MAPTemplateCRDSpec `json:"spec,omitempty"`
}

// MAPTemplateCRDSpec defines the CRD spec for the generated MutationConstraint kind.
type MAPTemplateCRDSpec struct {
	// Names defines the resource and kind names for the generated CRD.
	// Reuses the same Names type from the constraint framework.
	Names templates.Names `json:"names,omitempty"`

	// Validation defines the OpenAPI v3 schema for MutationConstraint parameters.
	// Reuses the same Validation type from the constraint framework.
	Validation *templates.Validation `json:"validation,omitempty"`
}

// MAPPolicy contains the MutatingAdmissionPolicy spec fields that the
// MAPTemplate controller copies into the generated MAP resource.
type MAPPolicy struct {
	// MatchConstraints defines what resources this policy matches.
	// Copied directly to MAP.spec.matchConstraints.
	// Gatekeeper also merges scope sync selectors (webhook namespaceSelector,
	// objectSelector) into this field.
	// Since MAP reached Beta in Kubernetes 1.34, Gatekeeper uses the v1beta1
	// API types from admissionregistration.k8s.io. The API version detection
	// logic (IsMAPAPIEnabled) still checks v1 → v1beta1 → v1alpha1 at runtime
	// to support clusters at different Kubernetes versions.
	MatchConstraints *admregv1beta1.MatchResources `json:"matchConstraints,omitempty"`

	// Variables defines named CEL expressions that can be referenced in
	// other expressions (mutations, matchConditions). Gatekeeper appends
	// its own variables at reconcile time (params, anyObject) — user-defined
	// variables must not use the reserved "gatekeeper-" prefix.
	// +optional
	Variables []admregv1beta1.Variable `json:"variables,omitempty"`

	// MatchConditions is a list of CEL conditions that must be met for the
	// policy to apply. Gatekeeper injects additional conditions with the
	// reserved "gatekeeper-internal-" prefix for scope exclusions.
	// User-provided names with this prefix are rejected at creation time.
	// +optional
	MatchConditions []admregv1beta1.MatchCondition `json:"matchConditions,omitempty"`

	// Mutations defines the CEL mutation expressions.
	Mutations []admregv1beta1.Mutation `json:"mutations,omitempty"`

	// FailurePolicy defines how to handle CEL evaluation failures.
	// Defaults to Fail (CEL errors block admission).
	// +optional
	FailurePolicy *admregv1beta1.FailurePolicyType `json:"failurePolicy,omitempty"`

	// ReinvocationPolicy indicates whether mutations may be called multiple
	// times per binding. Allowed values: "Never", "IfNeeded".
	// +optional
	ReinvocationPolicy *admregv1beta1.ReinvocationPolicyType `json:"reinvocationPolicy,omitempty"`
}
```

#### MAPTemplate Status Types

**Location**: `apis/maptemplate/v1alpha1/types.go` (continued)

```go
// MAPTemplateStatus defines the observed state of MAPTemplate.
type MAPTemplateStatus struct {
	// Created indicates whether the MutationConstraint CRD was successfully created.
	Created bool `json:"created,omitempty"`

	// ByPod reports status from each controller pod.
	// Uses MAPTemplateByPodStatus (extends templates.ByPodStatus with
	// MAPGenerationStatus) so the top-level status surfaces MAP generation
	// state without requiring the user to inspect per-pod CRDs.
	ByPod []MAPTemplateByPodStatus `json:"byPod,omitempty"`
}

// MAPTemplateByPodStatus is the aggregated per-pod status surfaced on
// MAPTemplateStatus.ByPod.
type MAPTemplateByPodStatus struct {
	// ID is a unique identifier for the pod that wrote the status.
	ID string `json:"id,omitempty"`

	// ObservedGeneration is the MAPTemplate generation last processed by this pod.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Errors lists any CRD creation or MAP generation errors from this pod.
	// Reuses the same CreateCRDError type from the constraint framework
	// (github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1).
	Errors []*templatesv1beta1.CreateCRDError `json:"errors,omitempty"`

	// MAPGenerationStatus tracks the state of MAP/CRD generation for this pod.
	// +optional
	MAPGenerationStatus *MAPGenerationStatus `json:"mapGenerationStatus,omitempty"`
}

// MAPGenerationStatus represents the status of MAP/CRD generation.
type MAPGenerationStatus struct {
	// State is "generated" or "error".
	State string `json:"state,omitempty"`

	// ObservedGeneration is the MAPTemplate generation last processed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Warning contains non-fatal generation warnings (e.g., deprecated API version).
	Warning string `json:"warning,omitempty"`
}
```

#### MutationConstraint Status Types

MutationConstraint instances are dynamically generated CRDs (unstructured at runtime),
analogous to how Constraints are unstructured instances of ConstraintTemplate-generated
CRDs. The controller writes these well-known status fields to the unstructured object.

**Location**: `apis/status/v1beta1/mutationconstraintpodstatus_types.go`

```go
// MutationConstraintStatus defines the status fields written to MutationConstraint
// instances by the MutationConstraint controller. Since MutationConstraint CRDs are
// dynamically generated, these fields are set on unstructured objects.
type MutationConstraintStatus struct {
	// ByPod reports MAPB generation status from each controller pod.
	// Aggregated by the mutationconstraintstatus controller from
	// MutationConstraintPodStatus per-pod CRDs.
	ByPod []MutationConstraintByPodStatus `json:"byPod,omitempty"`
}

// MutationConstraintByPodStatus is the per-pod status aggregated onto
// MutationConstraint.status.byPod.
type MutationConstraintByPodStatus struct {
	// ID is a unique identifier for the pod that wrote the status.
	ID string `json:"id,omitempty"`

	// ObservedGeneration is the MutationConstraint generation last processed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Errors lists any MAPB generation errors from this pod.
	Errors []MutationConstraintError `json:"errors,omitempty"`

	// MAPBGenerationStatus tracks the state of MAPB generation for this pod.
	// +optional
	MAPBGenerationStatus *MAPBGenerationStatus `json:"mapbGenerationStatus,omitempty"`
}

// MAPBGenerationStatus represents the status of MAPB generation for a
// MutationConstraint instance.
type MAPBGenerationStatus struct {
	// State is "generated", "error", or "waiting" (BlockMAPBGenerationUntil
	// delay not yet expired).
	State string `json:"state,omitempty"`

	// ObservedGeneration is the MutationConstraint generation last processed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Message contains error or wait details (e.g., "waiting for CRD cache",
	// "MAPTemplate not found").
	Message string `json:"message,omitempty"`
}
```

#### Per-Pod Status Resources

Following Gatekeeper's distributed status pattern (same as `ConstraintTemplatePodStatus`
and `ConstraintPodStatus`):

```go
// MAPTemplatePodStatus tracks MAP/CRD generation per controller pod.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
type MAPTemplatePodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status MAPTemplatePodStatusStatus `json:"status,omitempty"`
}

type MAPTemplatePodStatusStatus struct {
	ID                  string              `json:"id,omitempty"`
	TemplateUID         types.UID           `json:"templateUID,omitempty"`
	Operations          []string            `json:"operations,omitempty"`
	ObservedGeneration  int64               `json:"observedGeneration,omitempty"`
	Errors              []*templatesv1beta1.CreateCRDError   `json:"errors,omitempty"`
	MAPGenerationStatus *MAPGenerationStatus `json:"mapGenerationStatus,omitempty"`
}

// MutationConstraintPodStatus tracks MAPB generation per controller pod.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
type MutationConstraintPodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status MutationConstraintPodStatusStatus `json:"status,omitempty"`
}

// MutationConstraintError represents an error encountered during MAPB generation.
type MutationConstraintError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
}

type MutationConstraintPodStatusStatus struct {
	ID                   string                `json:"id,omitempty"`
	ConfigUID            types.UID             `json:"configUID,omitempty"`
	Operations           []string              `json:"operations,omitempty"`
	ObservedGeneration   int64                 `json:"observedGeneration,omitempty"`
	Errors               []MutationConstraintError `json:"errors,omitempty"`
	MAPBGenerationStatus *MAPBGenerationStatus `json:"mapbGenerationStatus,omitempty"`
}

// Standard List types (MAPTemplateList, MAPTemplatePodStatusList,
// MutationConstraintPodStatusList) follow the kubebuilder list pattern.
```

Generated MAP and MAPB names use a deterministic `gatekeeper-<name>` prefix
(e.g., `gatekeeper-k8salwayspullimages`). Generated CRDs use the standard
`<plural>.<group>` naming without the prefix (e.g.,
`k8salwayspullimages.mutationconstraints.gatekeeper.sh`).

---

### YAML Examples

#### MAPTemplate Instance

```yaml
apiVersion: templates.gatekeeper.sh/v1alpha1
kind: MAPTemplate
metadata:
  name: k8salwayspullimages
spec:/
  crd:
    sp
      names:
        kind: K8sAlwaysPullImages
      validation:
        openAPIV3Schema:
          type: object
          properties:
            imagePullPolicy:
              type: string
              enum: ["Always", "IfNotPresent", "Never"]
  policy:
    matchConstraints:
      resourceRules:
        - apiGroups: [""]
          apiVersions: ["v1"]
          operations: ["CREATE", "UPDATE"]
          resources: ["pods"]
    matchConditions:
      - name: exclude-system
        expression: "!object.metadata.namespace.startsWith('kube-')"
    mutations:
      - patchType: ApplyConfiguration
        applyConfiguration:
          expression: |
            Object{
              spec: Object.spec{
                containers: object.spec.containers.map(c,
                  Object.spec.containers.item{
                    name: c.name,
                    imagePullPolicy: variables.params.imagePullPolicy
                  }
                )
              }
            }
    failurePolicy: Fail
    reinvocationPolicy: IfNeeded
```

#### Generated MutationConstraint CRD

The MAPTemplate controller generates a CRD in `mutationconstraints.gatekeeper.sh` with owner
reference to the MAPTemplate. Key structural decisions in the generated schema:

```yaml
# Auto-generated — key structure (standard CRD boilerplate omitted)
metadata:
  name: k8salwayspullimages.mutationconstraints.gatekeeper.sh
  ownerReferences: [{kind: MAPTemplate, name: k8salwayspullimages, controller: true}]
spec:
  group: mutationconstraints.gatekeeper.sh
  names: {kind: K8sAlwaysPullImages, plural: k8salwayspullimages}
  scope: Cluster
  versions:
    - name: v1alpha1
      schema:
        openAPIV3Schema:
          # spec.match — namespaceSelector, objectSelector (for MAPB scoping)
          # spec.parameters — from template validation schema (user params)
          # spec.parameterNotFoundAction — enum: Allow|Deny, default: Allow
          # status.byPod — x-kubernetes-preserve-unknown-fields (aggregated status)
```

#### MutationConstraint Instance (User-Created)

```yaml
apiVersion: mutationconstraints.gatekeeper.sh/v1alpha1
kind: K8sAlwaysPullImages
metadata:
  name: always-pull-production
spec:
  match:
    namespaceSelector:
      matchLabels:
        environment: production
  parameters:
    imagePullPolicy: "Always"
  # parameterNotFoundAction: Allow  # optional, default: Allow
status:
  byPod:
    - id: gatekeeper-controller-manager-abc123
      observedGeneration: 1
      mapbGenerationStatus:
        state: "generated"
        observedGeneration: 1
```

---

### Generated Resources

This section shows only the Gatekeeper-injected fields. Template-authored fields
(`matchConstraints`, `mutations`, `matchConditions`, etc.) are copied verbatim from
the MAPTemplate and are omitted below for brevity.

**MutatingAdmissionPolicy** (owned by MAPTemplate):
```yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingAdmissionPolicy
metadata:
  name: gatekeeper-k8salwayspullimages           # gatekeeper-<template-name>
  ownerReferences:
    - {kind: MAPTemplate, name: k8salwayspullimages, controller: true, blockOwnerDeletion: true}
spec:
  paramKind:
    apiVersion: mutationconstraints.gatekeeper.sh/v1alpha1
    kind: K8sAlwaysPullImages                     # from template.spec.crd.spec.names.kind
  variables:                                       # Gatekeeper-injected (prepended to user variables)
    - name: params
      expression: "!has(params.spec) ? null : !has(params.spec.parameters) ? null: params.spec.parameters"
    - name: anyObject                              # coerces object/oldObject for DELETE support
      expression: 'has(request.operation) && request.operation == "DELETE" && object == null ? oldObject : object'
  matchConstraints:
    # ... user-defined resourceRules from template ...
    namespaceSelector: {}                          # merged from mutation webhook config
    objectSelector: {}                             # merged from mutation webhook config
  matchConditions:
    # ... user-defined matchConditions from template ...
    - name: gatekeeper-internal-exclude-namespaces # Gatekeeper-injected scope exclusion
      expression: |
        [object, oldObject].exists(obj,
          obj != null && (
            !has(obj.metadata.namespace) || obj.metadata.namespace == "" ? true :
              !(obj.metadata.namespace in ['excluded-ns'])
          )
        )
  # mutations, failurePolicy, reinvocationPolicy: copied from template
```

**MutatingAdmissionPolicyBinding** (owned by MutationConstraint):
```yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingAdmissionPolicyBinding
metadata:
  name: gatekeeper-always-pull-production         # gatekeeper-<instance-name>
  ownerReferences:
    - {kind: K8sAlwaysPullImages, name: always-pull-production, controller: true, blockOwnerDeletion: true}
spec:
  policyName: gatekeeper-k8salwayspullimages      # references the generated MAP
  paramRef:
    name: always-pull-production                   # references the MutationConstraint instance
    parameterNotFoundAction: Allow                 # Gatekeeper default (see Open Questions)
  matchResources:                                  # from MutationConstraint.spec.match
    namespaceSelector:
      matchLabels:
        environment: production
```

---

## Controller Architecture

> **Design principle**: The MAPTemplate/MutationConstraint controller architecture
> mirrors the existing ConstraintTemplate/Constraint pattern
> (`pkg/controller/constrainttemplate/`, `pkg/controller/constraint/`). Where this
> document says a pattern is "the same as CT/constraint", refer to those controllers
> as the reference implementation. Differences are called out explicitly.

### MAPTemplate Controller

**Location**: `pkg/controller/maptemplate/`

**Operation Guard**: The `Add()` method checks `operations.IsAssigned(operations.Generate)` 
as its entry guard. Unlike the ConstraintTemplate controller (which uses
`HasValidationOperations()` — checking Audit/Status/Webhook), MAPTemplate controllers
are generation-only and have no policy evaluation or audit role.

**CEL Expression Validation**: The MAPTemplate controller does **not** compile or validate
user-provided CEL expressions at creation time. The generated MAP resource's CEL is
validated by the **Kubernetes API server** at MAP creation time:

- Invalid CEL in `spec.policy.mutations` or `spec.policy.matchConditions` will cause
  the generated MAP to be rejected by the API server
- The MAPTemplate controller surfaces this as an error in `status.byPod[].errors`
- No separate CEL compilation step is needed because Gatekeeper does not evaluate
  MAP CEL expressions itself — it only passes them through to the API server

**Initialization**:

The MAPTemplate controller's `newReconciler()` instantiates the following (same pattern
as `constrainttemplate_controller.go`):

- Shared `GenericEvent` channel (capacity 1024) for MutationConstraint events
- Two `WatchManager` registrars: `watcher` (MutationConstraints) and `statusWatcher` (statuses)
- Subordinate `MutationConstraint` controller via `mutationconstraint.Adder{Events, IfWatching, GetPod}`
- If `operations.Status` is assigned: `MutationConstraintStatus` aggregation controller
  and `MAPTemplateStatus` aggregation controller

**Controller watches**:

| # | Source | Handler | Purpose |
|---|--------|---------|---------|
| 1 | `MAPTemplate` | `EnqueueRequestForObject` | Direct MAPTemplate changes |
| 2 | `source.Channel(mapTemplateEvents)` | `EnqueueRequestForObject` | Scope sync events (Config/webhook changes). Always enabled when Generate is assigned — no opt-out flag (see Design Rationale below) |
| 3 | `MAPTemplatePodStatus` | `PodStatusToMAPTemplateMapper` | Status changes → re-reconcile MAPTemplate |
| 4 | `CustomResourceDefinition` | `EnqueueRequestForOwner(MAPTemplate)` | Owned CRD modified externally → re-reconcile |
| 5 | `MutatingAdmissionPolicy` | `ownerRefToMAPTemplate` map func | Owned MAP modified externally → re-reconcile (only if MAP API enabled) |

**Reconciliation (creation/update path)**:
```
1. Receive MAPTemplate event
2. Detect available Kubernetes MAP API version (cached, same as IsVapAPIEnabled pattern)
3. Validate matchCondition names (reject gatekeeper-internal-* prefix in user conditions)
4. Generate MutationConstraint CRD from template.spec.crd (with owner reference to MAPTemplate)
5. Set BlockMAPBGeneration annotations on MAPTemplate (two annotations — see below)
6. Register dynamic watch via watcher.AddWatch(gvk) AND statusWatcher.AddWatch(gvk)
7. Build MutatingAdmissionPolicy from template.spec.policy
8. Inject scope sync conditions (excluded namespaces, webhook selectors, etc.)
9. Create/Update MAP with owner reference to MAPTemplate
10. After MAP **creation or deletion only**, trigger MutationConstraint reconciliation for
    all instances of this kind (triggering MAPB generation or re-evaluation of the
    BlockMAPBGeneration delay). MAP **updates do not** trigger MutationConstraint
    reconciliation — this mirrors the existing VAP pattern where `triggerConstraintEvents()`
    fires only on VAP create/delete, not on update (scope sync updates to the MAP spec
    do not require MAPB re-generation since MAPBs reference the MAP by name, not by content).
11. Update MAPTemplate pod status
```

**Reconciliation (deletion path)**:

The MAPTemplate controller detects deletion via `IsNotFound` or a non-zero
`DeletionTimestamp`. No finalizers are used.

```
1. Detect deletion: IsNotFound error on Get, or DeletionTimestamp is set
2. Delete metrics: r.metrics.DeleteMAPStatus(...)
3. Remove dynamic watches: r.watcher.RemoveWatch(ctx, gvk) AND
   r.statusWatcher.RemoveWatch(ctx, gvk)
4. Cancel readiness tracking: r.tracker.CancelTemplate(template)
5. Explicitly delete MAP by deterministic name: gatekeeper-<template-name>
   (ignores NotFound — MAP may already be garbage collected via owner ref)
6. Delete per-pod status resources for this template
7. Return early (skip creation/update path)
```

### MutationConstraint Controller

**Location**: `pkg/controller/mutationconstraint/`

**Operation Guard**: Only runs when `operations.IsAssigned(operations.Generate)` returns true.

**Created by**: MAPTemplate controller's `newReconciler()` (subordinate controller pattern).

**Controller watches**:

| # | Source | Handler | Purpose |
|---|--------|---------|---------|
| 1 | `source.Channel(events)` | `EventPackerMapFunc` | Dynamic MutationConstraint events from WatchManager |
| 2 | `MutationConstraintPodStatus` | Status mapper | Status changes → re-reconcile MutationConstraint |
| 3 | `MutatingAdmissionPolicyBinding` | `eventPackerMapFuncFromOwnerRefs` | Owned MAPB modified externally → re-reconcile (only if MAP API enabled) |

**MAPTemplate lookup mechanism**: The MutationConstraint controller determines the corresponding
MAPTemplate by converting the MutationConstraint's Kind to lowercase:

```go
// Fetch the MAPTemplate that generated this MutationConstraint's CRD
mapTemplate := &MAPTemplate{}
err := r.reader.Get(ctx, types.NamespacedName{Name: strings.ToLower(instance.GetKind())}, mapTemplate)
```

**Reconciliation**:
```
1. Receive MutationConstraint event (via dynamic watch registered by MAPTemplate controller)
2. Guard with IfWatching(gvk, fn) — skip events for GVKs no longer watched
3. Fetch corresponding MAPTemplate via strings.ToLower(instance.GetKind())
4. Check BlockMAPBGeneration annotations — two annotations:
   a. gatekeeper.sh/block-mapb-generation-until (RFC3339 timestamp)
   b. gatekeeper.sh/mapb-generation-state ("blocked" or "unblocked")
   If state is "blocked" and timestamp has not expired, requeue with remaining delay
5. Build MutatingAdmissionPolicyBinding:
   - policyName: gatekeeper-<maptemplate-name>
   - paramRef.name: <mutationconstraint-instance-name>
   - parameterNotFoundAction: from MutationConstraint.spec.parameterNotFoundAction (default: Allow)
   - matchResources: from MutationConstraint.spec.match (namespaceSelector, objectSelector)
6. Create/Update MAPB with owner reference to MutationConstraint instance
7. Update MutationConstraint pod status
```

**Reconciliation (deletion path)**:

The MutationConstraint controller detects deletion via `IsNotFound`,
`IsNoMatchError` (CRD deleted), `IfWatching` returning `false`, or a non-zero
`DeletionTimestamp`. No finalizers are used.

```
1. Detect deletion: IfWatching returns false, IsNotFound/IsNoMatchError, or DeletionTimestamp set
2. Delete MAPB metrics: r.reporter.DeleteMAPBStatus(...)
3. Cancel readiness expectations: tracker.For(gvk).CancelExpect(instance)
4. Delete per-pod status: delete MutationConstraintPodStatus for the current pod
5. Explicitly delete MAPB by deterministic name: gatekeeper-<instance-name>
   (ignores NotFound — MAPB may already be garbage collected via owner ref)
6. Return early (skip creation/update path)
```

### Shared Controller Patterns

Both controllers follow these patterns from the CT/constraint architecture.
See [Ownership and Lifecycle](#ownership-and-lifecycle) for the full ownership chain
and deletion cascade semantics.

**CRD caching race mitigation**: MAPTemplate sets two annotations:
- `gatekeeper.sh/block-mapb-generation-until` — RFC3339 timestamp = `now + 30s`
- `gatekeeper.sh/mapb-generation-state` — initially `"blocked"`, flipped to `"unblocked"`
  once expired. Retry-on-conflict for concurrent access.

The MutationConstraint controller checks both: if blocked and unexpired → requeue with
remaining delay. Configurable via `--default-wait-for-mapb-generation` flag (default: 30s).
The 30s delay is for API server CRD caching, NOT watch registration (which is event-driven
and immediate).

**Retry behavior after delay expires**: Following the existing VAP pattern
(`BlockVAPBGenerationUntilAnnotation` in the constraint controller), once the timestamp
expires the controller flips `mapb-generation-state` to `"unblocked"` and proceeds with
MAPB generation **without further blocking**. If the CRD is still not cached at that
point, the MAPB `Create`/`Update` call may fail with a `NoMatchError` — the controller
records the error in `MutationConstraintPodStatus` and relies on the standard controller-
runtime requeue-on-error to retry. No infinite retry loop occurs because the annotation
state is `"unblocked"` and the delay check is skipped on subsequent reconciliations.

**Dynamic watch registration**: MAPTemplate calls `watcher.AddWatch(gvk)` +
`statusWatcher.AddWatch(gvk)` → WatchManager starts informer → sends `GenericEvent` →
MutationConstraint controller processes via `IfWatching(gvk, fn)` guard +
`EventPackerMapFunc`/`UnpackRequest` for GVK encoding.

**MutationConstraint re-triggering**: After MAP create/update/delete, MAPTemplate lists all
MutationConstraint instances and sends `GenericEvent` for each (via
`triggerMutationConstraintEvents()`). Necessary
because MAPB generation depends on MAP existence and the BlockMAPBGeneration delay
countdown starts from MAP creation.

**Scope reconciliation storms**: Mitigated by generation-counter no-op detection,
scope-condition diffing before MAP writes, early exit on MAP API unavailability,
and potential batched/debounced sync for 200+ templates.

### Status Aggregation Controllers

Two status controllers follow Gatekeeper's distributed status pattern (mirroring
`ConstraintTemplateStatus` and `ConstraintStatus`). Both run only when
`operations.IsAssigned(operations.Status)` returns true.

| | MAPTemplateStatus | MutationConstraintStatus |
|---|---|---|
| **Location** | `pkg/controller/maptemplatestatus/` | `pkg/controller/mutationconstraintstatus/` |
| **CT/Constraint equivalent** | `ConstraintTemplateStatus` | `ConstraintStatus` |
| **Watch source** | `source.Kind` only (static CRDs) | `source.Kind` + `source.Channel` (dynamic GVKs) |
| **Pod status type** | `MAPTemplatePodStatus` | `MutationConstraintPodStatus` |
| **Parent resource** | `MAPTemplate` | MutationConstraint (unstructured) |
| **Key labels** | `MAPTemplateNameLabel` | `MutationConstraintNameLabel` + `MutationConstraintKindLabel` |
| **GVK handling** | N/A (static) | `EventPackerMapFunc` + `UnpackRequest` + `IfWatching` guard |
| **Adder deps** | None | `WatchManager`, `Events` channel, `IfWatching` func |

**Shared reconciliation flow**: Fetch parent → list pod statuses by label in Gatekeeper
namespace → sort by ID, filter stale (UID mismatch) → aggregate into
`parent.status.byPod` → update status. MAPTemplateStatus additionally sets
`template.status.created = true` if any pod reported zero errors.

MutationConstraintStatus additionally validates `gvk.Group == "mutationconstraints.gatekeeper.sh"`
and uses `IfWatching` to guard reads for dynamically registered GVKs.

---

## Scope Synchronization

### Design Rationale: No Opt-Out Flag

Unlike the existing VAP integration (which has `--sync-vap-enforcement-scope`), MAP scope
synchronization has **no opt-out flag**. Scope sync is always active when the Generate
operation is assigned. The reasons are:

1. **MAP is a brand-new feature** — there is no existing behavior to protect with a
   gradual rollout flag. VAP needed the flag because scope sync changed the behavior of
   already-deployed VAP resources.
2. **MAPTemplate exists solely for MAP generation** — unlike ConstraintTemplate (which
   serves validation, audit, webhook, and generation), MAPTemplate has no purpose without
   MAP generation. There is no scenario where you'd want MAPs created but not scoped.
3. **Not syncing scope is a security issue** — MAPs without scope sync could apply
   mutations to namespaces excluded from Gatekeeper's mutation webhook (e.g.,
   `kube-system`), violating the operator's configured exclusion intent.
4. **The VAP flag is being removed** — `--sync-vap-enforcement-scope` is planned for
   removal in a future release, making scope sync unconditional for VAP too. Adding
   a new flag that will immediately need deprecation is unnecessary.

### Event Channel Wiring

Scope sync requires an event channel (`mapTemplateEvents`) to propagate Config and
webhook changes to the MAPTemplate controller. This follows the existing `CtEvents`
pattern:

1. **`main.go`**: Creates the channel when Generate operation is assigned:
   ```go
   if operations.IsAssigned(operations.Generate) {
       opts.MapTemplateEvents = make(chan event.GenericEvent, 1024)
   }
   ```

2. **`controller.go`**: Defines a `MAPTemplateEventInjector` interface and injects the
   channel into all controllers that implement it:
   ```go
   type MAPTemplateEventInjector interface {
       InjectMAPTemplateEvent(mapTemplateEvents chan event.GenericEvent)
   }
   ```

3. **Injected into**: Config controller, `MutationWebhookConfigController`, and MAPTemplate
   controller. Config and `MutationWebhookConfigController` write events to the channel;
   MAPTemplate controller reads from it.

### How Config Changes Trigger MAPTemplate Reconciliation

The Config controller (`pkg/controller/config/config_controller.go`) checks whether
excluded namespaces changed for the relevant process scope before triggering:

```go
// In Config controller Reconcile():
if operations.IsAssigned(operations.Generate) && r.mapTemplateEvents != nil {
    // Only trigger if excluder actually changed for the mutation webhook process
    configChanged := r.cacheManager.ExcluderChangedForProcess(process.Mutation, newExcluder)
    if configChanged {
        // Trigger reconciliation for ALL MAPTemplates
        r.triggerMAPTemplateReconciliation(ctx)
    } else {
        // Retry only previously failed ("dirty") templates
        r.triggerDirtyMAPTemplateReconciliation(ctx)
    }
}
```

The trigger function lists all MAPTemplates and sends a `GenericEvent` for each one
(with retry-on-channel-full, matching the existing `sendEventWithRetry` pattern).

### How Webhook Config Changes Trigger MAPTemplate Reconciliation

**Location**: `pkg/controller/mutationwebhookconfig/`

A **new** controller (separate from the existing VAP `WebhookConfigController`) watches
`MutatingWebhookConfiguration` for Gatekeeper's mutation webhook. Only runs when
`operations.IsAssigned(operations.Generate)` returns true. Watches
`MutatingWebhookConfiguration` via `source.Kind`.

**Reconciliation flow**:
1. Fetch `MutatingWebhookConfiguration` (if deleted → remove cache entry → trigger all MAPTemplates)
2. Find Gatekeeper's mutation webhook entry by name (`--mutating-webhook-configuration-name`,
   default: `"gatekeeper-mutating-webhook-configuration"` — mirrors `--validating-webhook-configuration-name`
   defined in `pkg/webhook/common.go`)
3. Extract `namespaceSelector`, `objectSelector`, `rules`, `matchPolicy`, `matchConditions`
4. Compare with `WebhookConfigCache` — only trigger MAPTemplate reconciliation if changed

**RBAC**: Requires `get`, `list`, `watch` on `mutatingwebhookconfigurations` in
`admissionregistration.k8s.io`.

Event channel wiring and injector interfaces (`MAPTemplateEventInjector`,
`MutationWebhookConfigCacheInjector`) follow the same pattern described in
[Event Channel Wiring](#event-channel-wiring). Additionally, `main.go` creates a
`WebhookConfigCache` instance and wires it through dependency injection.

### Scope Injection

Gatekeeper automatically injects scope restrictions into generated MAPs:

| Source | Injected As |
|--------|-------------|
| Config.spec.match.excludedNamespaces | matchCondition CEL expression (`gatekeeper-internal-exclude-namespaces`) |
| `--exempt-namespace` flag | matchCondition CEL expression (`gatekeeper-internal-exempt-namespace`) |
| `--exempt-namespace-prefix` flag | matchCondition CEL expression (`gatekeeper-internal-exempt-namespace-prefix`) |
| `--exempt-namespace-suffix` flag | matchCondition CEL expression (`gatekeeper-internal-exempt-namespace-suffix`) |
| Webhook namespaceSelector | MAP matchConstraints.namespaceSelector |
| Webhook objectSelector | MAP matchConstraints.objectSelector |

**Note**: Scope sync sources are the **mutation webhook** configuration, not the
validating webhook. The existing VAP integration syncs from the validating webhook
because VAP replaces validation. MAP replaces mutation, so it should sync from the
mutating webhook (`--mutating-webhook-configuration-name`, default:
`"gatekeeper-mutating-webhook-configuration"` — see `pkg/webhook/common.go`).

**Cluster-scoped resource handling**: All Gatekeeper-injected namespace exclusion
matchConditions use the VAP-proven pattern of checking
`!has(obj.metadata.namespace) || obj.metadata.namespace == ""` before applying
namespace-based filtering. Cluster-scoped resources (which have no namespace) always
pass these conditions and are never incorrectly excluded. See the
`matchGlobalExcludedNamespacesGlob` pattern in
`pkg/drivers/k8scel/transform/cel_snippets.go` for the existing implementation.
The generated MAP example in [Generated Resources](#generated-resources) demonstrates
this pattern.

```go
// Pseudo-code for scope injection
func (r *ReconcileMAPTemplate) transformTemplateToMAP(
    template *MAPTemplate,
    mapName string,
) (*MutatingAdmissionPolicy, error) {
    // Scope sync is always active — no opt-out flag (see Design Rationale)
    var excludedNamespaces []string
    if r.processExcluder != nil {
        excludedNamespaces = r.processExcluder.GetExcludedNamespaces(process.Mutation)
    }
    exemptedNamespaces := webhook.GetAllExemptedNamespacesWithWildcard()
    webhookConfig := r.getWebhookConfigFromCache() // mutation webhook config

    return buildFromTemplateWithWebhookConfig(
        template, webhookConfig, excludedNamespaces, exemptedNamespaces,
    )
}
```

---

## Ownership and Lifecycle

All generated resources use **both** owner references (Kubernetes GC as safety net)
**and** explicit deletion (for immediate cleanup). See the MAPTemplate and
MutationConstraint deletion paths in [Controller Architecture](#controller-architecture)
for the full reconciliation flows.

```
MAPTemplate
├── owns → MutationConstraint CRD
└── owns → MutatingAdmissionPolicy

MutationConstraint (instance)
└── owns → MutatingAdmissionPolicyBinding
```

- Deleting `MAPTemplate` → owner ref GC deletes MAP + CRD → CRD deletion triggers
  Kubernetes built-in CR garbage collection for all MutationConstraint instances →
  owner ref GC deletes all MAPBs
- Deleting `MutationConstraint` instance → owner ref GC deletes its MAPB

**Warning**: Deleting MAPTemplate cascade-deletes ALL MutationConstraint instances of
that type (same behavior as ConstraintTemplate deletion).

---

## Kubernetes Version Compatibility

### API Version Detection

```go
func IsMAPAPIEnabled() (bool, *schema.GroupVersion) {
    // Check in order: v1 → v1beta1 → v1alpha1
    // Return first available version
    // Cache result for subsequent calls
}

func BuildMAPForVersion(template *MAPTemplate, gv *schema.GroupVersion) runtime.Object {
    // Build version-appropriate MAP object
}
```

> **Known Issue**: A cluster upgrade that enables the MAP API will not be detected until
> Gatekeeper restarts. A separate GitHub issue should be opened to add TTL-based cache
> expiration for both VAP and MAP API version detection.

---

## Security Considerations

### Privilege Escalation via MAPTemplate

**Critical**: Unlike VAP (validation-only), MAP can **modify** resources. A user with
`create` permission on `MAPTemplate` can define CEL mutations that inject arbitrary
content into matched resources (sidecar containers, RBAC bindings, secret mounts, etc.).

**Mitigation**: Gatekeeper should ensure that MAPTemplate creation requires equivalent
privileges to directly creating a `MutatingAdmissionPolicy`. This can be enforced via:

1. **RBAC**: Only grant `create`/`update` on `maptemplates` to users who also have
   `create`/`update` on `mutatingadmissionpolicies` in `admissionregistration.k8s.io`.
2. **Admission Webhook**: An optional validating webhook on MAPTemplate that performs
   a SubjectAccessReview to verify the requesting user could create a MAP directly.
3. **Documentation**: Clearly document that MAPTemplate authoring is a cluster-admin
   level privilege due to the mutation capability.

### RBAC Requirements

```yaml
rules:
  - apiGroups: ["admissionregistration.k8s.io"]
    resources: ["mutatingadmissionpolicies", "mutatingadmissionpolicybindings"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["admissionregistration.k8s.io"]
    resources: ["mutatingwebhookconfigurations"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["templates.gatekeeper.sh"]
    resources: ["maptemplates", "maptemplates/status"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["mutationconstraints.gatekeeper.sh"]
    # Wildcard required: controller must manage dynamically generated MutationConstraint CRDs
    # whose resource names are not known at deployment time.
    resources: ["*"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### matchCondition Name Protection

System-injected matchConditions use the reserved `gatekeeper-internal-` prefix.
User-provided matchConditions with this prefix are rejected at MAPTemplate creation time
to prevent shadowing of scope exclusion conditions.

---

## Known Limitations

1. **Template Deletion Cascade**: Deleting MAPTemplate deletes ALL MutationConstraint instances of that type (follows same behavior as existing ConstraintTemplate cleanup — CRD deletion removes all CRs of that kind)
2. **MAP API Unavailable**: MAPTemplate creation succeeds but MAP generation fails (error in status)
3. **Scope Sync Triggers**: Config/webhook changes trigger reconciliation of ALL MAPTemplates (mitigated by generation counter / no-op detection, and early exit if MAP API is unavailable on the cluster)
4. **No Dry-Run**: No way to test MAP mutations without affecting resources
5. **Parallel Systems**: Users may accidentally create both existing mutators and MAP for same resources (see Open Questions)
6. **API Version Cache**: Cached permanently until Gatekeeper restart (tracked as separate issue)

---

## Open Questions

### Coexistence with Existing Gatekeeper Mutators

Users may have both Gatekeeper's existing mutation webhook (Assign, AssignMetadata, ModifySet,
AssignImage) and MAP policies active simultaneously, potentially targeting the same resources.
When this happens:

- Both systems execute independently, potentially applying conflicting mutations
- `reinvocationPolicy: IfNeeded` on MAP means webhook mutations trigger MAP reinvocation
  and vice versa
- Kubernetes limits reinvocations (typically max 2 passes), preventing infinite loops
- Final mutation order is non-deterministic and depends on webhook ordering

**Options**:
1. **Do nothing** — document the risk, recommend full migration per resource type
2. **Advisory warning** — detect overlap at reconcile time, set warning in MAPTemplate status
3. **Disable flag** — `--disable-mutation-webhook-when-map-enabled`
4. **Block creation** — reject overlapping MAPTemplates via admission webhook
5. **Automatic exclusion** — inject matchConditions to skip webhook-processed resources

Needs user feedback from teams planning incremental migration.

### Feature Gate Cleanup / Orphaned Resources

If the MAP feature gate is disabled or the API group is removed after MAP resources have
been created, the API server will reject DELETE requests for MAP/MAPB resources. This means
Gatekeeper cannot clean them up automatically — they become orphaned.

**Options**:
1. **Do nothing** — deterministic names (`gatekeeper-<name>`) let admins identify orphans manually
2. **Record in status** — track generated resource names for cleanup discovery
3. **Pre-deletion check** — proactively delete MAP/MAPB while API is still available
4. **Finalizer-based cleanup** — attempt cleanup before MAPTemplate deletion

Likely a non-issue once MAP reaches GA and the feature gate is removed.

### `parameterNotFoundAction` Default

The Kubernetes MAP API defaults `parameterNotFoundAction` to `Deny` when the field is
unset. The current design overrides this to `Allow` in the generated MAPB to prevent
cluster-wide admission failures when param resources are temporarily unavailable (etcd
blips, CRD cache eviction, cascade deletion ordering).

However, this diverges from the Kubernetes default and could surprise users who expect
MAP behavior to match the upstream API semantics.

**Options**:
1. **Keep `Allow` default** (current design) — prioritizes stability, opt-in to `Deny`
2. **Match Kubernetes default (`Deny`)** — upstream-consistent, risk of transient outages
3. **Global flag** — `--default-parameter-not-found-action=Allow|Deny`, per-instance override
4. **Require explicit value** — make it required on MutationConstraint CRD

### MAPTemplate Update Semantics

Same behavior as ConstraintTemplate CRD schema updates: in-place CRD `Update()`, existing
MutationConstraint instances left as-is (schema validation delegated to API server at
future admission time). MutationConstraint reconciliation is **not** triggered after CRD
schema changes or MAP updates — only after MAP creation or deletion (mirroring the VAP
pattern where `triggerConstraintEvents()` fires on create/delete, not update). MAPBs
reference the MAP by name, so MAP content changes don't invalidate them.

Schema-breaking MAPTemplate updates can leave stale MutationConstraint instances that
users must manually update or recreate.

### Webhook Config Controller Deduplication

The design introduces a new `MutationWebhookConfigController` that watches
`MutatingWebhookConfiguration`, separate from the existing `WebhookConfigController`
that watches `ValidatingWebhookConfiguration` for VAP scope sync. Both controllers
follow the same pattern: watch a webhook configuration resource, extract matching
config (selectors, rules), cache it via `WebhookConfigCache`, and trigger template
reconciliation when relevant fields change.

**Options to consider**:
1. **Keep separate controllers for Phase 1, extract shared base later** (recommended) —
   start with a separate `MutationWebhookConfigController` that duplicates the existing
   `WebhookConfigController` pattern. This avoids regression risk to the proven VAP scope
   sync path during initial MAP development. Once both controllers are stabilized, extract
   a generic `WebhookConfigWatcher` parameterized by webhook type (`Validating`/`Mutating`),
   target event channel, and webhook name flag. The ~80% code overlap makes this
   refactoring straightforward but it should be done as a follow-up PR, not intermixed
   with the MAP feature implementation.
2. **Extract a shared parameterized base upfront** — create the generic
   `WebhookConfigWatcher` immediately and have both VAP and MAP instantiate it. Reduces
   duplication from day one but requires modifying the existing, tested VAP code path
   in the same PR that adds MAP support, increasing risk and review complexity.

---

## Future Work

1. **Gator CLI**: Test MAPTemplate/MutationConstraint resources
2. **Metrics**: Prometheus metrics for MAP generation success/failure
3. **Policy Library**: Add MAP policies to gatekeeper-library
4. **Admission Validation**: Webhook to reject MAPTemplate if MAP API unavailable

---

## Alternatives Considered

### 1. Extend Existing Mutator CRDs

Add MAP generation to Assign, AssignMetadata, etc.

**Rejected**: Requires complex translation from path-based syntax to CEL. Different mental models.

### 2. Direct MAP Generation Without CRD Intermediary

Allow users to create MAP/MAPB resources directly via a single CRD (no MutationConstraint
intermediary). Gatekeeper would inject scope sync into user-authored MAPs.

**Rejected**: Loses the template/instance separation that enables reusable policy
definitions with per-namespace parameterization. Users would need to duplicate MAP
resources for each parameter set. The MAPTemplate → MutationConstraint pattern provides
the same template/instance UX that ConstraintTemplate → Constraint already established.

### 3. Reuse ConstraintTemplate With a Mutation Mode

Add a `mode: mutation` field to ConstraintTemplate, generating MAP instead of VAP when
set.

**Rejected**: ConstraintTemplate's `spec.targets[].rego` / `spec.targets[].code` schema
is validation-oriented (returns violations). MAP requires `mutations` (CEL apply
configurations), which is a fundamentally different spec structure. Overloading
ConstraintTemplate would create a confusing API surface where most fields are
mode-dependent. A separate CRD provides clearer semantics and independent versioning.

---

## Implementation Plan

| Phase | Scope |
|-------|-------|
| 1a | **CRD Definitions**: MAPTemplate CRD, MutationConstraint CRD, MAPTemplatePodStatus CRD, MutationConstraintPodStatus CRD. RBAC rules for all new resource types. |
| 1b | **MAPTemplate Controller Core**: Reconciler with deletion handling (explicit MAP/CRD delete + watch removal + status cleanup), MutationConstraint CRD generation with owner references, MAP generation with owner references, API version detection (`IsMAPAPIEnabled`), matchCondition name validation (`gatekeeper-internal-` prefix rejection), MAPB generation delay (two annotations: timestamp + state). |
| 1c | **MutationConstraint Controller Core**: Subordinate controller initialization from MAPTemplate controller's `newReconciler()`, dynamic watch registration via WatchManager (two registrars: watcher + statusWatcher), `EventPacker` pattern for MutationConstraint events, `IfWatching` guard, MAPB generation with owner references, deletion handling (explicit MAPB delete + status cleanup). |
| 1d | **Status Aggregation Controllers**: MAPTemplateStatus controller with direct `source.Kind` watches, MutationConstraintStatus controller with `source.Channel` + `IfWatching` for dynamic GVKs, `MAPTemplateEventInjector` interface, shared utility package (`pkg/policygeneration/`). |
| 1e | **Phase 1 Testing**: Unit tests for all Phase 1 components (CRD generation, MAP/MAPB construction, deletion flows, status aggregation, event packing, annotation handling). |
| 2 | **Scope Synchronization**: Event channel wiring in `main.go` and `controller.go`, Config controller integration (`ExcluderChangedForProcess(process.Mutation, ...)`), new `MutationWebhookConfigController` (`pkg/controller/mutationwebhookconfig/`) watching `MutatingWebhookConfiguration`, `MutationWebhookConfigCacheInjector` interface, `WebhookConfigCache` integration, scope injection into MAPs, `triggerMAPTemplateReconciliation` with dirty template tracking and `sendEventWithRetry`. Integration tests for scope sync. |
| 3 | **Safety & Governance**: Conflict detection warnings, feature gate cleanup logic, privilege escalation mitigations (SubjectAccessReview webhook), documentation. Integration tests for conflict detection. |
| 4 | **Observability & Performance**: Metrics (MAP/MAPB generation success/failure/status), enhanced error handling, performance optimization (batched/debounced scope sync, early exit on MAP API unavailability). |
| 5 | **CRD Versioning**: CRD conversion strategy (`v1alpha1` → `v1beta1` → `v1`): storage version bumps with conversion webhooks if schema changes. Additive changes (new optional fields) may not need conversion webhooks. MAP CRDs need their own conversion infrastructure since they are managed independently of the constraint framework. |

---

## References

- [KEP-3962: Mutating Admission Policies](https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/3962-mutating-admission-policies)
- [Gatekeeper VAP Integration](https://open-policy-agent.github.io/gatekeeper/website/docs/validating-admission-policy/)
- [Existing VAP Transform Code](../../pkg/drivers/k8scel/transform/make_vap_objects.go)
