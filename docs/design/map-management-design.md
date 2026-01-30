# Design Document: MutatingAdmissionPolicy Management Feature

**Issue**: [#4261](https://github.com/open-policy-agent/gatekeeper/issues/4261)  
**Author**: Jaydip Gabani  
**Status**: Draft  
**Created**: January 30, 2026  

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
13. [Future Work](#future-work)
14. [Alternatives Considered](#alternatives-considered)

---

## Overview

This document describes the design for adding MutatingAdmissionPolicy (MAP) management capabilities to Gatekeeper. Similar to how Gatekeeper currently manages ValidatingAdmissionPolicy (VAP), ValidatingAdmissionPolicyBinding, and param resources from ConstraintTemplates and Constraints, this feature will introduce new CRDs (`MutationTemplate` and `MutationConstraint`) to manage MAP, MutatingAdmissionPolicyBinding, and param resources.

This feature enables users to leverage Kubernetes-native MutatingAdmissionPolicy through Gatekeeper's familiar CRD-based workflow, providing a consistent experience across validation and mutation policies.

---

## Goals and Non-Goals

### Goals

1. **Introduce new CRDs** (`MutationTemplate`, `MutationConstraint`) for managing MAP resources
2. **Follow established patterns** from VAP integration (ConstraintTemplate → VAP, Constraint → VAPBinding)
3. **Pass-through CEL expressions** - Reference Kubernetes MAP spec directly without translation
4. **Sync enforcement scope** with Gatekeeper's mutating webhook configuration
5. **Support parameterization** via dedicated param CRD resources
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
MutationTemplate ──────────► MutatingAdmissionPolicy
        │                            │
        │                            │ (paramKind references)
        ▼                            ▼
MutationConstraint ─────────► MutatingAdmissionPolicyBinding
        │                            │
        │                            │ (paramRef points to)
        ▼                            ▼
  MutationParams ◄───────────── paramRef
  (dedicated CRD)
```

### Resource Flow

1. **MutationTemplate** controller:
   - Watches `MutationTemplate` resources
   - Generates corresponding `MutatingAdmissionPolicy` resources
   - Generates CRD for `MutationParams` (param resource schema)
   - Sets owner reference from MAP to MutationTemplate

2. **MutationConstraint** controller:
   - Watches `MutationConstraint` resources
   - Generates corresponding `MutatingAdmissionPolicyBinding` resources
   - Generates `MutationParams` instances from constraint spec
   - Sets owner reference from MAPBinding to MutationConstraint

---

## API Design

### MutationTemplate CRD

```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: MutationTemplate
metadata:
  name: always-pull-images
  annotations:
    # Optional: Override global flag per-template
    gatekeeper.sh/generate-map: "true"
spec:
  # CRD specification for the generated MutationParams
  crd:
    spec:
      names:
        kind: AlwaysPullImagesParams
        listKind: AlwaysPullImagesParamsList
        plural: alwayspullimagesparams
        singular: alwayspullimagesparams
      validation:
        openAPIV3Schema:
          type: object
          properties:
            imagePullPolicy:
              type: string
              enum: ["Always", "IfNotPresent", "Never"]
              default: "Always"

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
    
    # Mutations to apply
    mutations:
      - patchType: ApplyConfiguration
        applyConfiguration:
          expression: |
            Object{
              spec: Object.spec{
                containers: object.spec.containers.map(c,
                  Object.spec.containers.item{
                    name: c.name,
                    imagePullPolicy: params.imagePullPolicy
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
  # Minimal status initially
  created: true
  # Reference to generated MAP
  mapName: gatekeeper-always-pull-images
  # Any generation errors
  errors: []
```

### MutationConstraint CRD

```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: MutationConstraint
metadata:
  name: always-pull-images-production
  annotations:
    # Optional: Override global flag per-constraint
    gatekeeper.sh/generate-mapb: "true"
spec:
  # Reference to the MutationTemplate
  templateRef:
    name: always-pull-images
  
  # Enforcement action (similar to Constraint)
  enforcementAction: deny  # deny, warn, dryrun
  
  # Match criteria (intersects with template's matchConstraints)
  match:
    # Namespace selector
    namespaceSelector:
      matchLabels:
        environment: production
    
    # Object selector
    labelSelector:
      matchExpressions:
        - key: app.kubernetes.io/managed-by
          operator: NotIn
          values: ["helm"]
    
    # Excluded namespaces
    excludedNamespaces:
      - kube-system
      - gatekeeper-system
  
  # Parameters for this constraint instance
  # This becomes the MutationParams resource
  parameters:
    imagePullPolicy: "Always"

status:
  # Minimal status initially
  created: true
  # Reference to generated binding
  mapBindingName: gatekeeper-always-pull-images-production
  # Reference to generated params
  paramsName: always-pull-images-production-params
  errors: []
```

### MutationParams CRD (Generated)

The `MutationParams` CRD is dynamically generated based on the `MutationTemplate.spec.crd` specification. This follows the same pattern as how ConstraintTemplate generates Constraint CRDs.

```yaml
# Auto-generated CRD
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: alwayspullimagesparams.mutations.gatekeeper.sh
  ownerReferences:
    - apiVersion: mutations.gatekeeper.sh/v1alpha1
      kind: MutationTemplate
      name: always-pull-images
spec:
  group: mutations.gatekeeper.sh
  names:
    kind: AlwaysPullImagesParams
    listKind: AlwaysPullImagesParamsList
    plural: alwayspullimagesparams
    singular: alwayspullimagesparams
  scope: Cluster
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                imagePullPolicy:
                  type: string
                  enum: ["Always", "IfNotPresent", "Never"]
                  default: "Always"
```

### Generated MutatingAdmissionPolicy

```yaml
# Auto-generated by MutationTemplate controller
apiVersion: admissionregistration.k8s.io/v1  # or v1beta1/v1alpha1
kind: MutatingAdmissionPolicy
metadata:
  name: gatekeeper-always-pull-images
  labels:
    gatekeeper.sh/mutation-template: always-pull-images
  ownerReferences:
    - apiVersion: mutations.gatekeeper.sh/v1alpha1
      kind: MutationTemplate
      name: always-pull-images
      controller: true
      blockOwnerDeletion: true
spec:
  # ParamKind references the generated params CRD
  paramKind:
    apiVersion: mutations.gatekeeper.sh/v1alpha1
    kind: AlwaysPullImagesParams
  
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
                  imagePullPolicy: params.imagePullPolicy
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
# Auto-generated by MutationConstraint controller
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingAdmissionPolicyBinding
metadata:
  name: gatekeeper-always-pull-images-production
  labels:
    gatekeeper.sh/mutation-constraint: always-pull-images-production
  ownerReferences:
    - apiVersion: mutations.gatekeeper.sh/v1alpha1
      kind: MutationConstraint
      name: always-pull-images-production
      controller: true
      blockOwnerDeletion: true
spec:
  policyName: gatekeeper-always-pull-images
  
  paramRef:
    name: always-pull-images-production-params
    parameterNotFoundAction: Deny
  
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

## Controller Architecture

### New Controllers

Two new dedicated controllers will be created:

#### 1. MutationTemplate Controller

**Location**: `pkg/controller/mutationtemplate/`

**Responsibilities**:
- Watch `MutationTemplate` resources
- Generate/update `MutatingAdmissionPolicy` resources
- Generate/update CRDs for param resources
- Handle deletion via owner references
- Report status back to MutationTemplate

**Reconciliation Flow**:
```
1. Receive MutationTemplate event
2. Check if MAP generation is enabled (global flag + annotation)
3. Detect available Kubernetes MAP API version
4. Build MutatingAdmissionPolicy from template spec
5. Inject Gatekeeper scope sync match conditions
6. Create/Update MAP with owner reference
7. Generate param CRD from template.spec.crd
8. Update MutationTemplate status
```

#### 2. MutationConstraint Controller

**Location**: `pkg/controller/mutationconstraint/`

**Responsibilities**:
- Watch `MutationConstraint` resources
- Generate/update `MutatingAdmissionPolicyBinding` resources
- Generate/update param resource instances
- Handle deletion via owner references
- Report status back to MutationConstraint

**Reconciliation Flow**:
```
1. Receive MutationConstraint event
2. Check if MAPB generation is enabled (global flag + annotation)
3. Validate referenced MutationTemplate exists
4. Build MutatingAdmissionPolicyBinding from constraint spec
5. Create/Update param resource instance from constraint.spec.parameters
6. Create/Update MAPB with owner reference
7. Update MutationConstraint status
```

### Controller Registration

```go
// pkg/controller/mutationtemplate/add.go
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

When `--sync-map-enforcement-scope=true` (default), the following are synchronized from Gatekeeper's mutating webhook configuration:

1. **Excluded Namespaces** (from Config resource)
2. **Exempted Namespaces** (from `--exempt-namespaces` flag)
3. **Namespace Selector** (from webhook configuration)
4. **Object Selector** (from webhook configuration)
5. **Match Conditions** (from webhook configuration)

### Implementation

```go
// pkg/drivers/map/transform/scope_sync.go

func SyncScopeToMAP(
    template *MutationTemplate,
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

## Feature Flags

### Global Flags

```go
var (
    // Controls MutatingAdmissionPolicy generation from MutationTemplate
    DefaultGenerateMAP = flag.Bool(
        "default-create-map-for-mutation-templates",
        false,  // Opt-out by default
        "(alpha) Generate MutatingAdmissionPolicy resources from MutationTemplate. "+
            "Allowed values: false (default, do not generate), true (generate).",
    )
    
    // Controls MutatingAdmissionPolicyBinding generation from MutationConstraint
    DefaultGenerateMAPB = flag.Bool(
        "default-create-mapb-for-mutation-constraints",
        false,  // Opt-out by default
        "(alpha) Generate MutatingAdmissionPolicyBinding resources from MutationConstraint. "+
            "Allowed values: false (default, do not generate), true (generate).",
    )
    
    // Sync MAP enforcement scope with Gatekeeper's mutating webhook
    SyncMAPEnforcementScope = flag.Bool(
        "sync-map-enforcement-scope",
        true,
        "(alpha) Synchronize MutatingAdmissionPolicy enforcement scope with Gatekeeper's "+
            "mutating webhook configuration.",
    )
    
    // Default reinvocation policy for generated MAPs
    DefaultMAPReinvocationPolicy = flag.String(
        "default-map-reinvocation-policy",
        "IfNeeded",
        "(alpha) Default reinvocation policy for generated MutatingAdmissionPolicy. "+
            "Allowed values: Never, IfNeeded.",
    )
)
```

### Per-Resource Annotations

```yaml
# Override global flag per MutationTemplate
metadata:
  annotations:
    gatekeeper.sh/generate-map: "true"  # or "false"

# Override global flag per MutationConstraint
metadata:
  annotations:
    gatekeeper.sh/generate-mapb: "true"  # or "false"

# Override reinvocation policy per MutationTemplate
metadata:
  annotations:
    gatekeeper.sh/map-reinvocation-policy: "Never"  # or "IfNeeded"
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
MutationTemplate
├── owns → MutatingAdmissionPolicy
└── owns → MutationParams CRD

MutationConstraint
├── owns → MutatingAdmissionPolicyBinding
└── owns → MutationParams instance
```

### Deletion Behavior

When a `MutationTemplate` is deleted:
1. Kubernetes garbage collector deletes owned `MutatingAdmissionPolicy`
2. Kubernetes garbage collector deletes owned `MutationParams` CRD
3. CRD deletion cascades to delete all `MutationParams` instances
4. All related `MutatingAdmissionPolicyBinding` become orphaned (fail gracefully)

When a `MutationConstraint` is deleted:
1. Kubernetes garbage collector deletes owned `MutatingAdmissionPolicyBinding`
2. Kubernetes garbage collector deletes owned `MutationParams` instance

---

## Status and Observability

### Initial Minimal Status

For the first iteration, status is kept minimal:

```go
type MutationTemplateStatus struct {
    // Whether MAP was successfully created
    Created bool `json:"created,omitempty"`
    
    // Name of the generated MAP
    MAPName string `json:"mapName,omitempty"`
    
    // Generation errors
    Errors []string `json:"errors,omitempty"`
    
    // Last reconciliation time
    LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`
}

type MutationConstraintStatus struct {
    // Whether MAPB was successfully created
    Created bool `json:"created,omitempty"`
    
    // Name of the generated MAPB
    MAPBindingName string `json:"mapBindingName,omitempty"`
    
    // Name of the generated params resource
    ParamsName string `json:"paramsName,omitempty"`
    
    // Generation errors
    Errors []string `json:"errors,omitempty"`
    
    // Last reconciliation time
    LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`
}
```

### Future Status Enhancements

In future iterations:
- Per-pod status tracking (similar to `ConstraintTemplatePodStatus`)
- Detailed validation status
- Metrics for MAP generation success/failure

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
    
    // Try v1alpha1
    if ok, gv := checkMAPGroupVersion(clientset, admissionregistrationv1alpha1.SchemeGroupVersion); ok {
        return true, gv
    }
    
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
func BuildMAPForVersion(template *MutationTemplate, gv *schema.GroupVersion) (runtime.Object, error) {
    switch *gv {
    case admissionregistrationv1.SchemeGroupVersion:
        return buildMAPV1(template)
    case admissionregistrationv1beta1.SchemeGroupVersion:
        return buildMAPV1Beta1(template)
    case admissionregistrationv1alpha1.SchemeGroupVersion:
        return buildMAPV1Alpha1(template)
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
  - apiGroups: ["mutations.gatekeeper.sh"]
    resources: ["mutationtemplates", "mutationtemplates/status"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  - apiGroups: ["mutations.gatekeeper.sh"]
    resources: ["mutationconstraints", "mutationconstraints/status"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### Input Validation

- CEL expressions in `MutationTemplate.spec.policy.mutations` are passed through to Kubernetes API server for validation
- Gatekeeper performs basic schema validation but relies on Kubernetes for CEL compilation/validation
- Invalid CEL will be rejected by the API server when MAP is created

---

## Future Work

1. **Gator CLI Support**: Extend `gator` to test `MutationTemplate` and `MutationConstraint` resources
2. **Enhanced Status**: Add per-pod status tracking similar to `ConstraintTemplatePodStatus`
3. **Metrics**: Add Prometheus metrics for MAP generation success/failure rates
4. **Audit Integration**: Track MAP mutations in Gatekeeper's audit system
5. **UI Integration**: Gatekeeper dashboard support for MAP resources
6. **Policy Library**: Add MAP-based mutation policies to gatekeeper-library

---

## Alternatives Considered

### Alternative 1: Extend Existing Mutator CRDs

**Approach**: Add MAP generation capability to existing `Assign`, `AssignMetadata`, etc.

**Rejected because**:
- Would require complex translation from Gatekeeper's path-based syntax to CEL
- Different mental models - existing mutators are declarative paths, MAP is CEL expressions
- Risk of breaking existing workflows
- User request specifically for vanilla MAP support

### Alternative 2: Single CRD for Both Template and Constraint

**Approach**: One CRD that defines both MAP and MAPB together

**Rejected because**:
- Doesn't match established pattern from VAP integration
- Less flexible - can't reuse template across multiple constraints
- Violates separation of concerns (policy definition vs. policy binding)

### Alternative 3: Direct MAP/MAPB Creation Without Wrapper CRDs

**Approach**: Users create MAP/MAPB directly, Gatekeeper only manages params

**Rejected because**:
- Loses Gatekeeper's scope synchronization benefits
- Inconsistent with VAP integration pattern
- Users would need to manually handle Gatekeeper exclusions

---

## Implementation Plan

### Phase 1: Core CRDs and Controllers (MVP)
1. Define `MutationTemplate` and `MutationConstraint` CRDs
2. Implement `MutationTemplate` controller (MAP + param CRD generation)
3. Implement `MutationConstraint` controller (MAPB + param instance generation)
4. Add feature flags (opt-out by default)
5. Basic status reporting
6. API version auto-detection

### Phase 2: Scope Synchronization
1. Implement scope sync with mutating webhook config
2. Add `--sync-map-enforcement-scope` flag
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
