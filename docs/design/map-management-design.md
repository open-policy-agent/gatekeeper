# Design Document: MutatingAdmissionPolicy Management Feature

**Issue**: [#4261](https://github.com/open-policy-agent/gatekeeper/issues/4261)  
**Author**: Jaydip Gabani  
**Status**: Draft  
**Created**: January 30, 2026  
**Updated**: February 3, 2026  

---

## Table of Contents

1. [Overview](#overview)
2. [Goals and Non-Goals](#goals-and-non-goals)
3. [Background](#background)
4. [Proposal](#proposal)
5. [API Design](#api-design)
6. [Controller Architecture](#controller-architecture)
7. [Scope Synchronization](#scope-synchronization)
8. [Feature Flags](#feature-flags)
9. [Lifecycle Management](#lifecycle-management)
10. [Status and Observability](#status-and-observability)
11. [Kubernetes Version Compatibility](#kubernetes-version-compatibility)
12. [Security Considerations](#security-considerations)
13. [Known Limitations and Edge Cases](#known-limitations-and-edge-cases)
14. [Future Work](#future-work)
15. [Alternatives Considered](#alternatives-considered)

---

## Overview

This document describes the design for adding MutatingAdmissionPolicy (MAP) management capabilities to Gatekeeper. Similar to how Gatekeeper currently manages ValidatingAdmissionPolicy (VAP), ValidatingAdmissionPolicyBinding, and param resources from ConstraintTemplates and Constraints, this feature will introduce new CRDs (`MAPTemplate` and `MAPConstraint`) to manage MAP, MutatingAdmissionPolicyBinding, and param resources.

This feature enables users to leverage Kubernetes-native MutatingAdmissionPolicy through Gatekeeper's familiar CRD-based workflow, providing a consistent experience across validation and mutation policies.

---

## Goals and Non-Goals

### Goals

1. **Introduce new CRDs** (`MAPTemplate`, `MAPConstraint`) for managing MAP resources
2. **Follow established patterns** from VAP integration (ConstraintTemplate → VAP, Constraint → VAPBinding)
3. **Pass-through CEL expressions** - Reference Kubernetes MAP spec directly without translation
4. **Sync enforcement scope** with Gatekeeper's mutating webhook configuration
5. **Use MAPConstraint as param resource** - Simplify by using MAPConstraint directly as the paramRef target
6. **Auto-detect Kubernetes API version** to support both alpha and beta/GA MAP APIs
7. **Opt-out by default** - Feature disabled unless explicitly enabled
8. **Use owner references** for proper garbage collection

### Non-Goals

1. **NOT converting existing Gatekeeper mutators** (Assign, AssignMetadata, ModifySet, AssignImage) to MAP
2. **NOT replacing existing mutation webhook** - These are separate, parallel capabilities
3. **NOT providing fallback** if MAP generation fails
4. **NOT extending gator CLI** for MAP testing (future work)
5. **NOT providing migration tooling** from existing mutators to MAP

---

## Background

### Kubernetes MutatingAdmissionPolicy (KEP-3962)

MutatingAdmissionPolicy (MAP) is a Kubernetes-native alternative to mutating webhooks, using CEL expressions for in-process mutation. Key characteristics:

| Version | Kubernetes Release | Status |
|---------|-------------------|--------|
| Alpha   | v1.32             | Feature gate required |
| Beta    | v1.34             | Enabled by default |
| GA      | v1.36             | Stable |

MAP consists of:
- **MutatingAdmissionPolicy**: Defines the mutation logic (CEL expressions, match constraints)
- **MutatingAdmissionPolicyBinding**: Binds policies to param resources and defines scope
- **Param Resources**: Optional custom resources providing runtime configuration

### Existing VAP Integration in Gatekeeper

Gatekeeper's VAP integration provides a proven pattern:

```
ConstraintTemplate ──────► ValidatingAdmissionPolicy
        │                          │
        │                          │ (paramKind references)
        ▼                          ▼
   Constraint ───────────► ValidatingAdmissionPolicyBinding
   (also serves as                  │
    param resource)                 │ (paramRef points to)
                                    ▼
                               Constraint
```

This design applies the same pattern to mutation.

---

## Proposal

### High-Level Architecture

```
MAPTemplate ───────────────► MutatingAdmissionPolicy
        │                            │
        │ (generates CRD)            │ (paramKind references)
        ▼                            ▼
MAPConstraint ─────────────► MutatingAdmissionPolicyBinding
  (instance of                       │
   generated CRD,                    │ (paramRef points to)
   serves as param)                  ▼
                              MAPConstraint
```

**Resource Count**: 
- User creates: 2 resources (MAPTemplate, MAPConstraint)
- Gatekeeper generates: 3 resources (CRD for MAPConstraint, MAP, MAPB)
- **Total user-managed: 2** (same as VAP integration)

### Resource Flow

1. **MAPTemplate** controller:
   - Watches `MAPTemplate` resources
   - Generates CRD for the `MAPConstraint` kind (e.g., `K8sAlwaysPullImages`)
   - Generates corresponding `MutatingAdmissionPolicy` resources
   - Sets owner reference from MAP to MAPTemplate

2. **MAPConstraint** controller:
   - Watches `MAPConstraint` resources (dynamically generated CRDs)
   - Generates corresponding `MutatingAdmissionPolicyBinding` resources
   - The `MAPConstraint` itself serves as the param resource
   - Sets owner reference from MAPBinding to MAPConstraint

---

## API Design

### MAPTemplate CRD

```yaml
apiVersion: map.gatekeeper.sh/v1alpha1
kind: MAPTemplate
metadata:
  name: k8salwayspullimages
spec:
  # CRD specification for the generated MAPConstraint
  # This follows the same pattern as ConstraintTemplate
  crd:
    spec:
      names:
        kind: K8sAlwaysPullImages
        listKind: K8sAlwaysPullImagesList
        plural: k8salwayspullimages
        singular: k8salwayspullimages
      validation:
        openAPIV3Schema:
          type: object
          properties:
            # Parameters that can be configured per-constraint
            imagePullPolicy:
              type: string
              enum: ["Always", "IfNotPresent", "Never"]
              default: "Always"
            excludeImages:
              type: array
              items:
                type: string

  # Direct pass-through of MutatingAdmissionPolicy spec
  # Users write vanilla Kubernetes MAP spec here
  policy:
    # Match constraints for the policy
    matchConstraints:
      resourceRules:
        - apiGroups: [""]
          apiVersions: ["v1"]
          operations: ["CREATE", "UPDATE"]
          resources: ["pods"]
    
    # Match conditions (CEL expressions)
    matchConditions:
      - name: exclude-system-namespaces
        expression: "!object.metadata.namespace.startsWith('kube-')"
    
    # Mutations to apply (uses 'params' variable which references MAPConstraint)
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
    
    # Variables for reuse in expressions
    variables:
      - name: containerNames
        expression: "object.spec.containers.map(c, c.name)"
    
    # Failure policy
    failurePolicy: Fail
    
    # Reinvocation policy (can be overridden per-template)
    reinvocationPolicy: IfNeeded

status:
  # Follows VAPGenerationStatus pattern from ConstraintTemplate
  mapGenerationStatus:
    # State: "generated", "error", or "waiting"
    state: "generated"
    # Tracks which generation of spec this status reflects
    observedGeneration: 1
    # Warning messages (e.g., operation mismatch)
    warning: ""
  # Name of the generated MAP
  mapName: gatekeeper-k8salwayspullimages
  # Name of the generated CRD for MAPConstraints
  crdName: k8salwayspullimages.map.gatekeeper.sh
  # Errors encountered during generation
  errors: []
```

### MAPConstraint CRD (Generated)

The `MAPConstraint` CRD is dynamically generated based on the `MAPTemplate.spec.crd` specification. This follows the same pattern as how ConstraintTemplate generates Constraint CRDs.

```yaml
# Auto-generated CRD by MAPTemplate controller
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: k8salwayspullimages.map.gatekeeper.sh
  labels:
    gatekeeper.sh/map-template: k8salwayspullimages
  ownerReferences:
    - apiVersion: map.gatekeeper.sh/v1alpha1
      kind: MAPTemplate
      name: k8salwayspullimages
      controller: true
      blockOwnerDeletion: true
spec:
  group: map.gatekeeper.sh
  names:
    kind: K8sAlwaysPullImages
    listKind: K8sAlwaysPullImagesList
    plural: k8salwayspullimages
    singular: k8salwayspullimages
  scope: Cluster
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            metadata:
              type: object
            spec:
              type: object
              properties:
                # Match criteria (same as Constraint)
                match:
                  type: object
                  properties:
                    namespaceSelector:
                      type: object
                      # ... LabelSelector schema
                    labelSelector:
                      type: object
                      # ... LabelSelector schema
                    excludedNamespaces:
                      type: array
                      items:
                        type: string
                    includedNamespaces:
                      type: array
                      items:
                        type: string
                # Parameters from template
                imagePullPolicy:
                  type: string
                  enum: ["Always", "IfNotPresent", "Never"]
                  default: "Always"
                excludeImages:
                  type: array
                  items:
                    type: string
            status:
              type: object
              # ... status fields
```

### MAPConstraint Instance (User-Created)

```yaml
# User creates this - it's an instance of the generated CRD
apiVersion: map.gatekeeper.sh/v1alpha1
kind: K8sAlwaysPullImages
metadata:
  name: always-pull-images-production
  annotations:
    # Optional: Override global flag per-constraint
    gatekeeper.sh/generate-mapb: "true"
spec:
  # Match criteria (intersects with template's matchConstraints)
  match:
    namespaceSelector:
      matchLabels:
        environment: production
    labelSelector:
      matchExpressions:
        - key: app.kubernetes.io/managed-by
          operator: NotIn
          values: ["helm"]
    excludedNamespaces:
      - kube-system
      - gatekeeper-system
  
  # Parameters for this constraint instance
  # These are accessed via 'params.spec.imagePullPolicy' in CEL
  imagePullPolicy: "Always"
  excludeImages:
    - "gcr.io/distroless/*"

status:
  # Follows VAPGenerationStatus pattern from Constraint
  mapbGenerationStatus:
    # State: "generated", "error", "waiting", or "blocked"
    state: "generated"
    # Tracks which generation of spec this status reflects
    observedGeneration: 1
    # Warning messages
    warning: ""
  # Name of the generated MAPB
  mapBindingName: gatekeeper-always-pull-images-production
  # Errors encountered during generation
  errors: []
```

### Generated MutatingAdmissionPolicy

```yaml
# Auto-generated by MAPTemplate controller
apiVersion: admissionregistration.k8s.io/v1  # or v1beta1/v1alpha1
kind: MutatingAdmissionPolicy
metadata:
  name: gatekeeper-k8salwayspullimages
  labels:
    gatekeeper.sh/map-template: k8salwayspullimages
  ownerReferences:
    - apiVersion: map.gatekeeper.sh/v1alpha1
      kind: MAPTemplate
      name: k8salwayspullimages
      controller: true
      blockOwnerDeletion: true
spec:
  # ParamKind references the generated MAPConstraint CRD
  paramKind:
    apiVersion: map.gatekeeper.sh/v1alpha1
    kind: K8sAlwaysPullImages
  
  # Match constraints from template + Gatekeeper scope sync
  matchConstraints:
    resourceRules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["pods"]
  
  # Match conditions include:
  # 1. Template-defined conditions
  # 2. Gatekeeper scope sync conditions (excluded namespaces, etc.)
  matchConditions:
    - name: exclude-system-namespaces
      expression: "!object.metadata.namespace.startsWith('kube-')"
    - name: gatekeeper-excluded-namespaces
      expression: '!(object.metadata.namespace in ["gatekeeper-system", "kube-system"])'
  
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
  
  variables:
    - name: containerNames
      expression: "object.spec.containers.map(c, c.name)"
  
  failurePolicy: Fail
  reinvocationPolicy: IfNeeded
```

### Generated MutatingAdmissionPolicyBinding

```yaml
# Auto-generated by MAPConstraint controller
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionPolicyBinding
metadata:
  name: gatekeeper-always-pull-images-production
  labels:
    gatekeeper.sh/map-constraint: always-pull-images-production
    gatekeeper.sh/map-constraint-kind: K8sAlwaysPullImages
  ownerReferences:
    - apiVersion: map.gatekeeper.sh/v1alpha1
      kind: K8sAlwaysPullImages
      name: always-pull-images-production
      controller: true
      blockOwnerDeletion: true
spec:
  policyName: gatekeeper-k8salwayspullimages
  
  # ParamRef points directly to the MAPConstraint instance
  paramRef:
    name: always-pull-images-production
    parameterNotFoundAction: Deny
  
  # Match resources from MAPConstraint.spec.match
  matchResources:
    namespaceSelector:
      matchLabels:
        environment: production
    objectSelector:
      matchExpressions:
        - key: app.kubernetes.io/managed-by
          operator: NotIn
          values: ["helm"]
```

---

## Status Types

Following the existing VAP status patterns in Gatekeeper, we define similar status structures for MAP management. The design mirrors:

### MAPGenerationStatus

Used in `MutationTemplatePodStatus.status.mapGenerationStatus` to track MAP and CRD generation. This mirrors `VAPGenerationStatus` exactly:

```go
// MAPGenerationStatus represents the status of MutatingAdmissionPolicy generation
type MAPGenerationStatus struct {
    // State indicates the generation state: "generated" or "error"
    State string `json:"state,omitempty"`
    
    // ObservedGeneration tracks which generation of the spec this status reflects
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    
    // Warning contains any warning messages (e.g., operation mismatch)
    Warning string `json:"warning,omitempty"`
}
```

**State Values**:
| State | Description |
|-------|-------------|
| `generated` | MAP and CRD were successfully generated |
| `error` | An error occurred during generation |

### MAPBGenerationStatus

```go
// MAPBGenerationStatus represents the status of MutatingAdmissionPolicyBinding generation
type MAPBGenerationStatus struct {
    // State indicates the generation state: "generated", "error", or "waiting"
    State string `json:"state,omitempty"`
    
    // ObservedGeneration tracks which generation of the spec this status reflects
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    
    // Message contains additional details (errors, waiting reasons, etc.)
    Message string `json:"message,omitempty"`
}
```

**State Values**:
| State | Description |
|-------|-------------|
| `generated` | MAPB was successfully generated |
| `error` | An error occurred during generation |
| `waiting` | Waiting for CRD caching in API server before generating MAPB |

> **Note**: Unlike validation Constraints which have multiple enforcement points (webhook + VAP), 
> MutationConstraints only generate MAPB - there is no "enforcement point" concept for mutation.

### State Constants

These constants follow the existing VAP pattern in Gatekeeper:

```go
const (
    // MutationTemplate states (mirrors constrainttemplate/constants.go)
    ErrGenerateMAPState  = "error"
    GeneratedMAPState    = "generated"
)

// pkg/controller/mutationconstraint/constants.go  
const (
    // MutationConstraint states
    ErrGenerateMAPBState = "error"
    GeneratedMAPBState   = "generated"
    WaitMAPBState        = "waiting"
)
```

---

## Controller Architecture

### New Controllers

Two new dedicated controllers will be created:

#### 1. MAPTemplate Controller

**Location**: `pkg/controller/maptemplate/`

**Responsibilities**:
- Watch `MAPTemplate` resources
- Generate/update CRDs for MAPConstraint kinds
- Generate/update `MutatingAdmissionPolicy` resources
- Handle deletion via owner references
- Report status back to MAPTemplate

**Reconciliation Flow**:
```
1. Receive MAPTemplate event
2. Check if MAP generation is enabled (global flag + annotation)
3. Detect available Kubernetes MAP API version
4. Generate MAPConstraint CRD from template.spec.crd
5. Build MutatingAdmissionPolicy from template.spec.policy
6. Inject Gatekeeper scope sync match conditions
7. Create/Update CRD and MAP with owner references
8. Update MAPTemplate status
```

**Dynamic CRD Generation** (same pattern as ConstraintTemplate):
```go
func (r *Reconciler) generateConstraintCRD(template *MAPTemplate) (*apiextensionsv1.CustomResourceDefinition, error) {
    crd := &apiextensionsv1.CustomResourceDefinition{
        ObjectMeta: metav1.ObjectMeta{
            Name: fmt.Sprintf("%s.map.gatekeeper.sh", strings.ToLower(template.Spec.CRD.Spec.Names.Plural)),
            Labels: map[string]string{
                "gatekeeper.sh/map-template": template.Name,
            },
        },
        Spec: apiextensionsv1.CustomResourceDefinitionSpec{
            Group: "map.gatekeeper.sh",
            Names: template.Spec.CRD.Spec.Names,
            Scope: apiextensionsv1.ClusterScoped,
            Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
                {
                    Name:    "v1alpha1",
                    Served:  true,
                    Storage: true,
                    Schema:  buildSchemaWithMatch(template.Spec.CRD.Spec.Validation),
                },
            },
        },
    }
    return crd, nil
}
```

#### 2. MAPConstraint Controller

**Location**: `pkg/controller/mapconstraint/`

**Responsibilities**:
- Watch MAPConstraint resources (dynamically generated CRDs)
- Generate/update `MutatingAdmissionPolicyBinding` resources
- Handle deletion via owner references
- Report status back to MAPConstraint

**Dynamic Watch Setup** (same pattern as Constraint controller):
```go
func (r *Reconciler) setupDynamicWatch(gvk schema.GroupVersionKind) error {
    // Register dynamic watch for the MAPConstraint CRD
    return r.watchManager.AddWatch(gvk, r.eventChannel)
}
```

**Reconciliation Flow**:
```
1. Receive MAPConstraint event
2. Check if MAPB generation is enabled (global flag + annotation)
3. Get corresponding MAPTemplate
4. Build MutatingAdmissionPolicyBinding from constraint spec
5. Set paramRef to point to this MAPConstraint instance
6. Create/Update MAPB with owner reference
7. Update MAPConstraint status
```

### Controller Registration

```go
// pkg/controller/maptemplate/add.go
func (a *Adder) Add(mgr manager.Manager) error {
    if !operations.HasValidationOperations() {
        return nil
    }
    // Only add if MAP feature is not explicitly disabled
    if !*DefaultGenerateMAP {
        return nil
    }
    // ... controller setup
}
```

---

## Scope Synchronization

### Synchronized Elements

1. **Excluded Namespaces** (from Config resource)
2. **Exempted Namespaces** (from `--exempt-namespaces` flag)
3. **Namespace Selector** (from webhook configuration)
4. **Object Selector** (from webhook configuration)
5. **Match Conditions** (from webhook configuration)

### Implementation

```go
// pkg/drivers/map/transform/scope_sync.go

func SyncScopeToMAP(
    template *MAPTemplate,
    webhookConfig *webhookconfigcache.WebhookMatchingConfig,
    excludedNamespaces []string,
    exemptedNamespaces []string,
) (*admissionregistrationv1.MutatingAdmissionPolicy, error) {
    
    policy := buildBasePolicyFromTemplate(template)
    
    // Add excluded namespace conditions
    if len(excludedNamespaces) > 0 {
        policy.Spec.MatchConditions = append(
            policy.Spec.MatchConditions,
            buildExcludedNamespacesCondition(excludedNamespaces),
        )
    }
    
    // Add exempted namespace conditions
    if len(exemptedNamespaces) > 0 {
        policy.Spec.MatchConditions = append(
            policy.Spec.MatchConditions,
            buildExemptedNamespacesCondition(exemptedNamespaces),
        )
    }
    
    // Sync webhook match conditions
    if webhookConfig != nil {
        policy.Spec.MatchConditions = append(
            policy.Spec.MatchConditions,
            convertWebhookMatchConditions(webhookConfig.MatchConditions)...,
        )
    }
    
    return policy, nil
}
```

---


## Lifecycle Management

### Owner References

All generated resources use owner references for garbage collection:

```go
func setOwnerReference(owner, owned metav1.Object, scheme *runtime.Scheme) error {
    return controllerutil.SetControllerReference(owner, owned, scheme)
}
```

**Ownership Chain**:
```
MAPTemplate
├── owns → MAPConstraint CRD (e.g., k8salwayspullimages.map.gatekeeper.sh)
└── owns → MutatingAdmissionPolicy

MAPConstraint (instance)
└── owns → MutatingAdmissionPolicyBinding
```

### Deletion Behavior

When a `MAPTemplate` is deleted:
1. Kubernetes garbage collector deletes owned `MutatingAdmissionPolicy`
2. Kubernetes garbage collector deletes owned `MAPConstraint` CRD
3. CRD deletion cascades to delete all `MAPConstraint` instances
4. Each instance deletion cascades to delete its `MutatingAdmissionPolicyBinding`

When a `MAPConstraint` instance is deleted:
1. Kubernetes garbage collector deletes owned `MutatingAdmissionPolicyBinding`

**Warning**: Deleting a MAPTemplate will cascade-delete ALL MAPConstraints of that type. Users should be warned about this in documentation.

---

## Status and Observability

Following Gatekeeper's per-pod status pattern, MAP management uses dedicated status resources for distributed status tracking.

### MutationTemplatePodStatus

Per-pod status for MutationTemplate, mirroring `ConstraintTemplatePodStatus`:

```go
// apis/status/v1beta1/mutationtemplatepodstatus_types.go

// MutationTemplatePodStatusStatus defines the observed state
type MutationTemplatePodStatusStatus struct {
    ID                  string                             `json:"id,omitempty"`
    TemplateUID         types.UID                          `json:"templateUID,omitempty"`
    Operations          []string                           `json:"operations,omitempty"`
    ObservedGeneration  int64                              `json:"observedGeneration,omitempty"`
    Errors              []*CreateCRDError                  `json:"errors,omitempty"`
    MAPGenerationStatus *MAPGenerationStatus               `json:"mapGenerationStatus,omitempty"`
}

// MAPGenerationStatus represents the status of MAP generation (mirrors VAPGenerationStatus)
type MAPGenerationStatus struct {
    State              string `json:"state,omitempty"`       // "generated" or "error"
    ObservedGeneration int64  `json:"observedGeneration,omitempty"`
    Warning            string `json:"warning,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
type MutationTemplatePodStatus struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Status MutationTemplatePodStatusStatus `json:"status,omitempty"`
}
```

### MutationConstraintPodStatus

Per-pod status for MutationConstraint instances, mirroring `ConstraintPodStatus`:

```go
// apis/status/v1beta1/mutationconstraintpodstatus_types.go

// MutationConstraintPodStatusStatus defines the observed state
type MutationConstraintPodStatusStatus struct {
    ID                   string                `json:"id,omitempty"`
    ConstraintUID        types.UID             `json:"constraintUID,omitempty"`
    Operations           []string              `json:"operations,omitempty"`
    Enforced             bool                  `json:"enforced,omitempty"`
    Errors               []Error               `json:"errors,omitempty"`
    ObservedGeneration   int64                 `json:"observedGeneration,omitempty"`
    MAPBGenerationStatus *MAPBGenerationStatus `json:"mapbGenerationStatus,omitempty"`
}

// MAPBGenerationStatus tracks MutatingAdmissionPolicyBinding generation
// (simple status - mutation has no multiple enforcement points like validation)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
type MutationConstraintPodStatus struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Status MutationConstraintPodStatusStatus `json:"status,omitempty"`
}
```

### Status Update Flow

**MutationTemplate Status Update:**
```go
func (r *Reconciler) updateMAPGenerationStatus(ctx context.Context, template *MutationTemplate, state, warning string) error {
    status, err := r.getOrCreatePodStatus(ctx, template)
    if err != nil {
        return err
    }
    
    status.Status.MAPGenerationStatus = &MAPGenerationStatus{
        State:              state,  // "generated" or "error"
        ObservedGeneration: template.GetGeneration(),
        Warning:            warning,
    }
    
    return r.client.Status().Update(ctx, status)
}
```

**MutationConstraint Status Update:**
```go
func updateMAPBGenerationStatus(status *MutationConstraintPodStatusStatus, state, message string, generation int64) {
    status.MAPBGenerationStatus = &MAPBGenerationStatus{
        State:              state,  // "generated", "error", or "waiting"
        Message:            message,
        ObservedGeneration: generation,
    }
}
```

### Example Status Objects

**MutationTemplatePodStatus Example:**
```yaml
apiVersion: status.gatekeeper.sh/v1beta1
kind: MutationTemplatePodStatus
metadata:
  name: gatekeeper-controller-manager-abc123-k8salwayspullimages
  namespace: gatekeeper-system
  labels:
    mutation-template-name: k8salwayspullimages
    pod: gatekeeper-controller-manager-abc123
status:
  id: gatekeeper-controller-manager-abc123
  templateUID: 12345-abcde
  operations: ["mutation-webhook"]
  observedGeneration: 1
  mapGenerationStatus:
    state: generated
    observedGeneration: 1
```

**MutationConstraintPodStatus Example:**
```yaml
apiVersion: status.gatekeeper.sh/v1beta1
kind: MutationConstraintPodStatus
metadata:
  name: gatekeeper-controller-manager-abc123-always-pull-images-production
  namespace: gatekeeper-system
status:
  id: gatekeeper-controller-manager-abc123
  constraintUID: 67890-fghij
  enforced: true
  observedGeneration: 1
  mapbGenerationStatus:
    state: generated
    observedGeneration: 1
```

---

## Kubernetes Version Compatibility

### API Version Detection

```go
// pkg/drivers/map/transform/map_util.go

var (
    mapMux sync.RWMutex
    MAPAPIEnabled *bool
    MAPGroupVersion *schema.GroupVersion
)

func IsMAPAPIEnabled(log *logr.Logger) (bool, *schema.GroupVersion) {
    mapMux.RLock()
    if MAPAPIEnabled != nil {
        apiEnabled, gv := *MAPAPIEnabled, MAPGroupVersion
        mapMux.RUnlock()
        return apiEnabled, gv
    }
    mapMux.RUnlock()
    
    mapMux.Lock()
    defer mapMux.Unlock()
    
    if MAPAPIEnabled != nil {
        return *MAPAPIEnabled, MAPGroupVersion
    }
    
    // Check for API availability in order of preference
    // 1. v1 (GA)
    // 2. v1beta1 (Beta)
    // 3. v1alpha1 (Alpha)
    
    cfg, err := config.GetConfig()
    if err != nil {
        log.Info("IsMAPAPIEnabled GetConfig", "error", err)
        MAPAPIEnabled = ptr.To(false)
        return false, nil
    }
    
    clientset, err := kubernetes.NewForConfig(cfg)
    if err != nil {
        log.Info("IsMAPAPIEnabled NewForConfig", "error", err)
        MAPAPIEnabled = ptr.To(false)
        return false, nil
    }
    
    // Try v1 first
    if ok, gv := checkMAPGroupVersion(clientset, admissionregistrationv1.SchemeGroupVersion); ok {
        return true, gv
    }
    
    // Try v1beta1
    if ok, gv := checkMAPGroupVersion(clientset, admissionregistrationv1beta1.SchemeGroupVersion); ok {
        return true, gv
    }
    
    // Try v1alpha1 (if types exist in client-go)
    // Note: v1alpha1 types may not be in client-go yet
    
    log.Error(nil, "MAP API not available in cluster")
    MAPAPIEnabled = ptr.To(false)
    return false, nil
}

func checkMAPGroupVersion(clientset *kubernetes.Clientset, gv schema.GroupVersion) (bool, *schema.GroupVersion) {
    resList, err := clientset.Discovery().ServerResourcesForGroupVersion(gv.String())
    if err != nil {
        return false, nil
    }
    for _, r := range resList.APIResources {
        if r.Name == "mutatingadmissionpolicies" {
            MAPAPIEnabled = ptr.To(true)
            MAPGroupVersion = &gv
            return true, MAPGroupVersion
        }
    }
    return false, nil
}
```

### Version-Specific Handling

```go
func BuildMAPForVersion(template *MAPTemplate, gv *schema.GroupVersion) (runtime.Object, error) {
    switch *gv {
    case admissionregistrationv1.SchemeGroupVersion:
        return buildMAPV1(template)
    case admissionregistrationv1beta1.SchemeGroupVersion:
        return buildMAPV1Beta1(template)
    default:
        return nil, fmt.Errorf("unsupported MAP API version: %s", gv.String())
    }
}
```

---

## Security Considerations

### RBAC Requirements

The Gatekeeper controller needs additional RBAC permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: gatekeeper-manager-role
rules:
  # Existing rules...
  
  # New rules for MAP management
  - apiGroups: ["admissionregistration.k8s.io"]
    resources: ["mutatingadmissionpolicies"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  - apiGroups: ["admissionregistration.k8s.io"]
    resources: ["mutatingadmissionpolicybindings"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  # Rules for new CRDs
  - apiGroups: ["map.gatekeeper.sh"]
    resources: ["maptemplates", "maptemplates/status"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  # Dynamic: rules for generated MAPConstraint CRDs
  - apiGroups: ["map.gatekeeper.sh"]
    resources: ["*"]  # Covers all generated MAPConstraint kinds
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### Input Validation

- CEL expressions in `MAPTemplate.spec.policy.mutations` are passed through to Kubernetes API server for validation
- Gatekeeper performs basic schema validation but relies on Kubernetes for CEL compilation/validation
- Invalid CEL will be rejected by the API server when MAP is created

---

## Known Limitations and Edge Cases

### 1. Template Deletion Cascade

Deleting a `MAPTemplate` will cascade-delete the generated CRD, which in turn deletes ALL `MAPConstraint` instances of that type. Users must be careful.

**Mitigation**: 
- Document this behavior prominently
- Consider adding a `preventDeletion` annotation
- Warn in status if constraints exist

### 2. MAP API Not Available

If the MAP feature gate is not enabled in Kubernetes, MAPTemplate creation will succeed but MAP generation will fail.

**Behavior**: Errors recorded in `MAPTemplate.status.errors`

**Future Enhancement**: Add validating webhook to reject MAPTemplate if MAP API unavailable

### 3. Scope Sync Trigger Loops

Changes to Config resource or webhook configuration trigger reconciliation of ALL MAPTemplates.

**Mitigation**: Rate limiting on reconciliation, batch updates

### 4. No Dry-Run Testing

Unlike validation policies, there's no way to test MAP mutations without affecting real resources.

**Future Work**: gator CLI support for MAP testing

### 5. Parallel Mutation Systems

Users might accidentally create both existing mutators (Assign, etc.) and MAP-based mutations for the same resources.

**Documentation**: Clearly document that these are separate, parallel systems

---

## Future Work

1. **Gator CLI Support**: Extend `gator` to test `MAPTemplate` and `MAPConstraint` resources
2. **Enhanced Status**: Add per-pod status tracking similar to `ConstraintTemplatePodStatus`
3. **Metrics**: Add Prometheus metrics for MAP generation success/failure rates
4. **Audit Integration**: Track MAP mutations in Gatekeeper's audit system
5. **UI Integration**: Gatekeeper dashboard support for MAP resources
6. **Policy Library**: Add MAP-based mutation policies to gatekeeper-library
7. **Admission Validation**: Webhook to reject MAPTemplate if MAP API unavailable

---

## Alternatives Considered

### Alternative 1: Extend Existing Mutator CRDs

**Approach**: Add MAP generation capability to existing `Assign`, `AssignMetadata`, etc.

**Rejected because**:
- Would require complex translation from Gatekeeper's path-based syntax to CEL
- Different mental models - existing mutators are declarative paths, MAP is CEL expressions
- Risk of breaking existing workflows
- User request specifically for vanilla MAP support

### Alternative 2: Separate Params CRD

**Approach**: Generate a separate MutationParams CRD instance from MAPConstraint.spec.parameters

**Rejected because**:
- Adds unnecessary complexity (6 resources instead of 4)
- Doesn't match VAP integration pattern where Constraint IS the param
- More moving parts for users to manage

### Alternative 3: Direct MAP/MAPB Creation Without Wrapper CRDs

**Approach**: Users create MAP/MAPB directly, Gatekeeper only manages params

**Rejected because**:
- Loses Gatekeeper's scope synchronization benefits
- Inconsistent with VAP integration pattern
- Users would need to manually handle Gatekeeper exclusions

---

## Implementation Plan

### Phase 1: Core CRDs and Controllers (MVP)
1. Define `MAPTemplate` CRD
2. Implement `MAPTemplate` controller (CRD + MAP generation)
3. Implement dynamic `MAPConstraint` controller (MAPB generation)
4. Add feature flags (opt-out by default)
5. Basic status reporting
6. API version auto-detection

### Phase 2: Scope Synchronization
1. Implement scope sync with mutating webhook config
3. Handle Config resource excluded namespaces
4. Handle exempt namespace flags

### Phase 3: Testing and Documentation
1. Unit tests for controllers and transforms
2. Integration tests with envtest
3. E2E tests with real Kubernetes cluster
4. Documentation and examples

### Phase 4: Hardening
1. Enhanced error handling
2. Metrics and observability
3. Edge case handling
4. Performance optimization

---

## References

- [KEP-3962: Mutating Admission Policies](https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/3962-mutating-admission-policies)
- [Gatekeeper VAP Integration](https://open-policy-agent.github.io/gatekeeper/website/docs/validating-admission-policy/)
- [Existing VAP Transform Code](../pkg/drivers/k8scel/transform/make_vap_objects.go)
- [Constraint Controller](../pkg/controller/constraint/constraint_controller.go)
- [ConstraintTemplate Controller](../pkg/controller/constrainttemplate/constrainttemplate_controller.go)
