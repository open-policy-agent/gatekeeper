# Design Document: MutatingAdmissionPolicy Management Feature

**Issue**: [#4261](https://github.com/open-policy-agent/gatekeeper/issues/4261)  
**Author**: Jaydip Gabani  
**Status**: Draft  
**Created**: January 30, 2026  
**Updated**: February 9, 2026  

---

## Table of Contents

1. [Overview](#overview)
2. [Goals and Non-Goals](#goals-and-non-goals)
3. [Background](#background)
4. [Proposal](#proposal)
5. [API Design](#api-design)
6. [Controller Architecture](#controller-architecture)
7. [Scope Synchronization](#scope-synchronization)
8. [Lifecycle Management](#lifecycle-management)
9. [Status and Observability](#status-and-observability)
10. [Kubernetes Version Compatibility](#kubernetes-version-compatibility)
11. [Security Considerations](#security-considerations)
12. [Known Limitations](#known-limitations)
13. [Future Work](#future-work)
14. [Alternatives Considered](#alternatives-considered)

---

## Overview

This document describes adding MutatingAdmissionPolicy (MAP) management to Gatekeeper. Similar to how ConstraintTemplates generate ValidatingAdmissionPolicy (VAP), this feature introduces `MAPTemplate` and `MAPConstraint` CRDs to manage MAP, MutatingAdmissionPolicyBinding (MAPB), and param resources.

---

## Goals and Non-Goals

### Goals

1. Introduce `MAPTemplate` and `MAPConstraint` CRDs for managing MAP resources
2. Follow established VAP integration patterns (Template → Policy, Constraint → Binding)
3. Pass-through CEL expressions directly to Kubernetes MAP spec
4. Sync enforcement scope with Gatekeeper's configuration automatically
5. Use `MAPConstraint` as the param resource (same pattern as Constraint → VAP)
6. Auto-detect Kubernetes API version (alpha/beta/GA)
7. Use owner references for garbage collection

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

### Existing VAP Pattern

```
ConstraintTemplate ──► ValidatingAdmissionPolicy
        │                       │ (paramKind)
        ▼                       ▼
   Constraint ─────────► VAPBinding (paramRef → Constraint)
```

This design applies the same pattern to mutation.

---

## Proposal

### Architecture

```
MAPTemplate ───────────► MutatingAdmissionPolicy
     │                          │ (paramKind)
     │ (generates CRD)          ▼
     ▼                    MAPBinding (paramRef)
MAPConstraint ─────────────────┘
```

**Resource Count**: User creates 2 (MAPTemplate, MAPConstraint), Gatekeeper generates 3 (CRD, MAP, MAPB)

### Resource Flow

1. **MAPTemplate controller** (runs in `generate` operation):
   - Generates CRD for MAPConstraint kind
   - Generates MutatingAdmissionPolicy with owner reference to MAPTemplate

2. **MAPConstraint controller** (runs in `generate` operation):
   - Watches dynamically generated MAPConstraint CRDs
   - Generates MutatingAdmissionPolicyBinding with owner reference to MAPConstraint

---

## API Design

### MAPTemplate CRD

```yaml
apiVersion: map.gatekeeper.sh/v1alpha1
kind: MAPTemplate
metadata:
  name: k8salwayspullimages
spec:
  crd:
    spec:
      names:
        kind: K8sAlwaysPullImages
      validation:
        openAPIV3Schema:
          type: object
          properties:
            imagePullPolicy:
              type: string
              enum: ["Always", "IfNotPresent", "Never"]

  # Direct pass-through of MAP spec
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
                    imagePullPolicy: params.spec.imagePullPolicy
                  }
                )
              }
            }
    failurePolicy: Fail
    reinvocationPolicy: IfNeeded

status:
  mapGenerationStatus:
    state: "generated"  # or "error"
    observedGeneration: 1
    warning: ""
  mapName: gatekeeper-k8salwayspullimages
  crdName: k8salwayspullimages.map.gatekeeper.sh
```

### Generated MAPConstraint CRD (pseudo-structure)

```yaml
# Auto-generated by MAPTemplate controller
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: k8salwayspullimages.map.gatekeeper.sh
  ownerReferences: [MAPTemplate/k8salwayspullimages]
spec:
  group: map.gatekeeper.sh
  names:
    kind: K8sAlwaysPullImages
  scope: Cluster
  versions:
    - name: v1alpha1
      schema:
        # Includes: spec.match (namespaceSelector, labelSelector, etc.)
        # Plus: template-defined parameters
```

### MAPConstraint Instance (User-Created)

```yaml
apiVersion: map.gatekeeper.sh/v1alpha1
kind: K8sAlwaysPullImages
metadata:
  name: always-pull-production
spec:
  match:
    namespaceSelector:
      matchLabels:
        environment: production
    excludedNamespaces: ["kube-system"]
  # Parameters accessed via params.spec.* in CEL
  imagePullPolicy: "Always"

status:
  mapbGenerationStatus:
    state: "generated"  # "generated", "error", or "waiting"
    observedGeneration: 1
    warning: ""
  mapBindingName: gatekeeper-always-pull-production
```

### Generated Resources (pseudo-structure)

**MutatingAdmissionPolicy:**
```yaml
metadata:
  name: gatekeeper-k8salwayspullimages
  ownerReferences: [MAPTemplate/k8salwayspullimages]
spec:
  paramKind:
    apiVersion: map.gatekeeper.sh/v1alpha1
    kind: K8sAlwaysPullImages
  matchConstraints: # from template + scope sync
  matchConditions: # from template + Gatekeeper exclusions
  mutations: # from template
```

**MutatingAdmissionPolicyBinding:**
```yaml
metadata:
  name: gatekeeper-always-pull-production
  ownerReferences: [K8sAlwaysPullImages/always-pull-production]
spec:
  policyName: gatekeeper-k8salwayspullimages
  paramRef:
    name: always-pull-production
    parameterNotFoundAction: Deny
  matchResources: # from MAPConstraint.spec.match
```

---

## Controller Architecture

### MAPTemplate Controller

**Location**: `pkg/controller/maptemplate/`

**Operation Guard**: Only runs when `operations.HasGenerateOperations()` returns true.

**Reconciliation**:
```
1. Receive MAPTemplate event
2. Detect available Kubernetes MAP API version
3. Generate MAPConstraint CRD from template.spec.crd
4. Build MutatingAdmissionPolicy from template.spec.policy
5. Inject scope sync conditions (excluded namespaces, etc.)
6. Create/Update CRD and MAP with owner references
7. Update MAPTemplate status
```

### MAPConstraint Controller

**Location**: `pkg/controller/mapconstraint/`

**Operation Guard**: Only runs when `operations.HasGenerateOperations()` returns true.

**Reconciliation**:
```
1. Receive MAPConstraint event (dynamic watch on generated CRDs)
2. Get corresponding MAPTemplate
3. Build MutatingAdmissionPolicyBinding
4. Set paramRef to this MAPConstraint instance
5. Create/Update MAPB with owner reference
6. Update MAPConstraint status
```

### Controller Registration

```go
func (a *Adder) Add(mgr manager.Manager) error {
    if !operations.HasGenerateOperations() {
        return nil
    }
    // ... controller setup
}
```

---

## Scope Synchronization

Gatekeeper automatically injects scope restrictions into generated MAPs:

| Source | Injected As |
|--------|-------------|
| Config.spec.match.excludedNamespaces | matchCondition CEL expression |
| `--exempt-namespaces` flag | matchCondition CEL expression |
| Webhook namespaceSelector | MAP matchConstraints.namespaceSelector |
| Webhook objectSelector | MAPB matchResources.objectSelector |

```go
// Pseudo-code for scope injection
func SyncScopeToMAP(template *MAPTemplate, config *Config) *MAP {
    policy := buildFromTemplate(template)
    
    // Add excluded namespace condition
    if len(config.ExcludedNamespaces) > 0 {
        policy.MatchConditions = append(policy.MatchConditions,
            buildNamespaceExclusionCondition(config.ExcludedNamespaces))
    }
    
    // Sync webhook selectors
    policy.MatchConstraints.NamespaceSelector = config.WebhookNamespaceSelector
    
    return policy
}
```

---

## Lifecycle Management

### Ownership Chain

```
MAPTemplate
├── owns → MAPConstraint CRD
└── owns → MutatingAdmissionPolicy

MAPConstraint (instance)
└── owns → MutatingAdmissionPolicyBinding
```

### Deletion Cascade

- Deleting `MAPTemplate` → deletes MAP + CRD → deletes all MAPConstraint instances → deletes all MAPBs
- Deleting `MAPConstraint` instance → deletes its MAPB

**Warning**: Deleting MAPTemplate cascade-deletes ALL MAPConstraints of that type.

---

## Status and Observability

### Status Types

```go
// MAPGenerationStatus - used in MAPTemplatePodStatus
type MAPGenerationStatus struct {
    State              string `json:"state,omitempty"`              // "generated" or "error"
    ObservedGeneration int64  `json:"observedGeneration,omitempty"`
    Warning            string `json:"warning,omitempty"`
}

// MAPBGenerationStatus - used in MAPConstraintPodStatus  
type MAPBGenerationStatus struct {
    State              string `json:"state,omitempty"`              // "generated", "error", or "waiting"
    ObservedGeneration int64  `json:"observedGeneration,omitempty"`
    Warning            string `json:"warning,omitempty"`
}
```

### Per-Pod Status Resources

Following Gatekeeper's distributed status pattern:

- **MAPTemplatePodStatus**: Tracks MAP/CRD generation per controller pod
- **MAPConstraintPodStatus**: Tracks MAPB generation per controller pod

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

---

## Security Considerations

### RBAC Requirements

```yaml
rules:
  - apiGroups: ["admissionregistration.k8s.io"]
    resources: ["mutatingadmissionpolicies", "mutatingadmissionpolicybindings"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["map.gatekeeper.sh"]
    resources: ["maptemplates", "maptemplates/status", "*"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### Input Validation

CEL expressions in `MAPTemplate.spec.policy` are passed through to Kubernetes API server. Invalid CEL is rejected when MAP is created.

---

## Known Limitations

1. **Template Deletion Cascade**: Deleting MAPTemplate deletes ALL MAPConstraints of that type
2. **MAP API Unavailable**: MAPTemplate creation succeeds but MAP generation fails (error in status)
3. **Scope Sync Triggers**: Config/webhook changes trigger reconciliation of ALL MAPTemplates
4. **No Dry-Run**: No way to test MAP mutations without affecting resources
5. **Parallel Systems**: Users may accidentally create both existing mutators and MAP for same resources

---

## Future Work

1. **Gator CLI**: Test MAPTemplate/MAPConstraint resources
2. **Metrics**: Prometheus metrics for MAP generation success/failure
3. **Audit Integration**: Track MAP mutations in audit system
4. **Policy Library**: Add MAP policies to gatekeeper-library
5. **Admission Validation**: Webhook to reject MAPTemplate if MAP API unavailable

---

## Alternatives Considered

### 1. Extend Existing Mutator CRDs

Add MAP generation to Assign, AssignMetadata, etc.

**Rejected**: Requires complex translation from path-based syntax to CEL. Different mental models.

### 2. Separate Params CRD

Generate separate param resource from MAPConstraint.spec.parameters.

**Rejected**: Adds complexity (6 resources vs 4). Doesn't match VAP pattern.

### 3. Direct MAP/MAPB Without Wrapper CRDs

Users create MAP/MAPB directly, Gatekeeper only manages params.

**Rejected**: Loses scope synchronization. Inconsistent with VAP integration.

---

## Implementation Plan

| Phase | Scope |
|-------|-------|
| 1 | Core CRDs, controllers, API version detection, basic status |
| 2 | Scope synchronization with Config and webhook config |
| 3 | Unit tests, integration tests, E2E tests, documentation |
| 4 | Metrics, enhanced error handling, performance optimization |

---

## References

- [KEP-3962: Mutating Admission Policies](https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/3962-mutating-admission-policies)
- [Gatekeeper VAP Integration](https://open-policy-agent.github.io/gatekeeper/website/docs/validating-admission-policy/)
- [Existing VAP Transform Code](../pkg/drivers/k8scel/transform/make_vap_objects.go)
