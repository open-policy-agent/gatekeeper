---
id: constrainttemplates
title: Constraint Templates
---

ConstraintTemplates define a way to validate some set of Kubernetes objects in Gatekeeper's Kubernetes [admission controller](https://kubernetes.io/blog/2019/03/21/a-guide-to-kubernetes-admission-controllers/).  They are made of two main elements:

1. [Rego](https://www.openpolicyagent.org/docs/latest/#rego) code that defines a policy violation
2. The schema of the accompanying `Constraint` object, which represents an instantiation of a `ConstraintTemplate`


## `v1` Constraint Template

In release version 3.6.0, Gatekeeper included the `v1` version of `ConstraintTemplate`.  Unlike past versions of `ConstraintTemplate`, `v1` requires the Constraint schema section to be [structural](https://kubernetes.io/blog/2019/06/20/crd-structural-schema/).

Structural schemas have a variety of [requirements](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#specifying-a-structural-schema).  One such requirement is that the `type` field be defined for each level of the schema.

For example, users of Gatekeeper may recognize the `k8srequiredlabels` ConstraintTemplate, defined here in version `v1beta1`:

```yaml
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
      validation:
        # Schema for the `parameters` field
        openAPIV3Schema:
          properties:
            labels:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels

        violation[{"msg": msg, "details": {"missing_labels": missing}}] {
          provided := {label | input.review.object.metadata.labels[label]}
          required := {label | label := input.parameters.labels[_]}
          missing := required - provided
          count(missing) > 0
          msg := sprintf("you must provide labels: %v", [missing])
        }
```

The `parameters` field schema (`spec.crd.spec.validation.openAPIV3Schema`) is _not_ structural.  Notably, it is missing the `type:` declaration:

```yaml
openAPIV3Schema:
  # missing type
  properties:
    labels:
      type: array
      items:
        type: string
```

This schema is _invalid_ by default in a `v1` ConstraintTemplate.  Adding the `type` information makes the schema valid:

```yaml
openAPIV3Schema:
  type: object
  properties:
    labels:
      type: array
      items:
        type: string
```

For more information on valid types in JSONSchemas, see the [JSONSchema documentation](https://json-schema.org/understanding-json-schema/reference/type.html).

## Why implement this change?

Structural schemas are required in version `v1` of `CustomResourceDefinition` resources, which underlie ConstraintTemplates.  Requiring the same in ConstraintTemplates puts Gatekeeper in line with the overall direction of Kubernetes.

Beyond this alignment, structural schemas yield significant usability improvements. The schema of a ConstraintTemplate's associated Constraint is both more visible and type validated.

As the data types of Constraint fields are defined in the ConstraintTemplate, the API server will reject a Constraint with an incorrect `parameters` field. Previously, the API server would ingest it and simply not pass those `parameters` to Gatekeeper.  This experience was confusing for users, and is noticeably improved by structural schemas.

For example, see this incorrectly defined `k8srequiredlabels` Constraint:

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: ns-must-have-gk
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
  parameters:
    # Note that "labels" is now contained in an array item, rather than an object key under "parameters"
    - labels: ["gatekeeper"]
```

In a `v1beta1` ConstraintTemplate, this Constraint would be ingested successfully.  However, it would not work.  The creation of a new namespace, `foobar`, would succeed, even in the absence of the `gatekeeper` label:

```shell
$ kubectl create ns foobar
namespace/foobar created
```

This is incorrect.  We'd expect this to fail:

```shell
$ kubectl create ns foobar
Error from server ([ns-must-have-gk] you must provide labels: {"gatekeeper"}): admission webhook "validation.gatekeeper.sh" denied the request: [ns-must-have-gk] you must provide labels: {"gatekeeper"}
```

The structural schema requirement _prevents this mistake_.  The aforementioned `type: object` declaration would prevent the API server from accepting the incorrect `k8srequiredlabels` Constraint.

```shell
# Apply the Constraint with incorrect parameters schema
$ cat << EOF | kubectl apply -f -
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: ns-must-have-gk
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
  parameters:
    # Note that "labels" is now an array item, rather than an object
    - labels: ["gatekeeper"]
EOF
The K8sRequiredLabels "ns-must-have-gk" is invalid: spec.parameters: Invalid value: "array": spec.parameters in body must be of type object: "array"
```

Fixing the incorrect `parameters` section would then yield a successful ingestion and a working Constraint.

```shell
$ cat << EOF | kubectl apply -f -
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: ns-must-have-gk
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
  parameters:
    labels: ["gatekeeper"]
EOF
k8srequiredlabels.constraints.gatekeeper.sh/ns-must-have-gk created
```

```shell
$ kubectl create ns foobar
Error from server ([ns-must-have-gk] you must provide labels: {"gatekeeper"}): admission webhook "validation.gatekeeper.sh" denied the request: [ns-must-have-gk] you must provide labels: {"gatekeeper"}
```

## Built-in variables across all engines

### Common variables

### Rego variables

| Variable | Description |
| --- | --- |
| `input.review` | Contains input request object under review |
| `input.parameters` | Contains constraint parameters e.g. `input.parameters.repos` see [example](https://open-policy-agent.github.io/gatekeeper-library/website/validation/allowedrepos) |
| `data.lib`     |  It serves as an import path for helper functions defined under `libs` in ConstraintTemplate, e.g. data.lib.exempt_container.is_exempt see [example](https://open-policy-agent.github.io/gatekeeper-library/website/validation/host-network-ports) |
| `data.inventory` | Refers to a structure that stores synced cluster resources. It is used in Rego policies to validate or enforce referential rules based on the current state of the cluster. e.g. unique ingress host [example](https://open-policy-agent.github.io/gatekeeper-library/website/validation/uniqueingresshost/) |

### CEL variables

| Variable | Description |
| --- | --- |
| `variables.params` | Contains constraint parameters e.g. `variables.params.labels` see [example](https://open-policy-agent.github.io/gatekeeper-library/website/validation/requiredlabels) |
| `variables.anyObject` | Contains either an object or (on DELETE requests) oldObject, see [example](https://open-policy-agent.github.io/gatekeeper-library/website/validation/requiredlabels) |

## Field Precedence in ConstraintTemplate

ConstraintTemplates support multiple ways to define policy code, and it's important to understand the precedence rules when multiple fields are specified.

### Schema Overview

The ConstraintTemplate schema provides the following fields under `spec.targets[]`:

- `spec.targets[].target` - The target name (e.g., `admission.k8s.gatekeeper.sh`)
- `spec.targets[].rego` - Legacy field for Rego code (deprecated, maintained for backward compatibility)
- `spec.targets[].code[]` - Array of code objects, each with:
  - `code[].engine` - The engine name (e.g., `Rego`, `K8sNativeValidation`)
  - `code[].source` - Engine-specific source code

### Precedence Rules

#### 1. Legacy `rego` Field Overwrites `code` Array Rego

If both `spec.targets[].rego` and a Rego engine entry in `spec.targets[].code[]` are specified, the **legacy `rego` field takes precedence** and overwrites the Rego code in the `code` array.

**Example:**

```yaml
spec:
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        # This Rego code will be used
        package example
        violation[{"msg": "from legacy rego field"}] { true }
      code:
        - engine: Rego
          source:
            rego: |
              # This Rego code will be IGNORED
              package example
              violation[{"msg": "from code array"}] { true }
```

In this example, the policy defined in the `rego` field will be evaluated, while the Rego code in the `code` array will be ignored.

**Best Practice:** Use only the `code` array for defining policies. The legacy `rego` field is maintained for backward compatibility with older ConstraintTemplate versions and should be avoided in new templates.

#### 2. CEL Engine Takes Precedence Over Rego Engine

When multiple engines are defined in the `code` array, **only one engine is evaluated** per ConstraintTemplate. The `K8sNativeValidation` (CEL) engine has higher priority than the `Rego` engine.

**Example:**

```yaml
spec:
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: K8sNativeValidation
          source:
            validations:
              - expression: "true"
                message: "CEL validation - this will be evaluated"
        - engine: Rego
          source:
            rego: |
              # This Rego code will NOT be evaluated
              package example
              violation[{"msg": "rego validation"}] { true }
```

In this example, only the CEL validation will be executed. The Rego policy will not be evaluated.

**Important Notes:**
- There is **no fallback mechanism** between engines. If the CEL policy has an error, the Rego policy will not be used as a backup.
- Violations or errors from the active engine (CEL in this case) are treated according to the `enforcementAction` specified in the Constraint.
- Engine priority cannot be modified or customized per ConstraintTemplate.

### Recommendations

1. **Use the `code` array**: Define all policy code using `spec.targets[].code[]` rather than the legacy `spec.targets[].rego` field.

2. **Choose one engine**: Define policy logic in only one engine (either Rego or CEL) to avoid confusion about which policy will be evaluated.

3. **Understand engine capabilities**: 
   - Use **Rego** for complex policies that require referential constraints, external data, or custom logic.
   - Use **CEL** for simpler validations that can benefit from in-process evaluation and potential integration with Kubernetes Validating Admission Policy.

For more information on CEL integration and engine precedence, see the [Integration with Kubernetes Validating Admission Policy](validating-admission-policy.md) documentation.

