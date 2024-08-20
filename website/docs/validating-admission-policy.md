---
id: validating-admission-policy
title: Integration with Kubernetes Validating Admission Policy
---

Validating Admission Policy CEL validation in Gatekeeper:
Feature State: Gatekeeper version v3.17 (beta)
❗ This feature is beta, subject to change (feedback is welcome!). It is enabled by default. Set --enable-k8s-native-validation=false` to disable evaluating Validating Admission Policy CEL in constraint templates.

VAP management through Gatekeeper:
Feature State: Gatekeeper version v3.16 (alpha)
❗ This feature is alpha, subject to change (feedback is welcome!). It is disabled by default unless explicitly enabled via feature flag and/or via constraint template.

## Description

This feature allows Gatekeeper to integrate with Kubernetes Validating Admission Policy based on [Common Expression Language (CEL)](https://github.com/google/cel-spec), a declarative, in-process admission control alternative to validating admission webhooks.

## Motivations

The Kubernetes Validating Admission Policy feature was introduced as an alpha feature to Kubernetes v1.26, beta in v1.28 (disabled by default), GA in v1.30 (enabled by default). Some of the benefits include:
- in-tree/native in-process
- reduce admission request latency
- improve reliability and availability
- able to fail closed without impacting availability
- avoid the operational burden of webhooks

To reduce policy fragmentation and simplify the user experience by standardizing the policy experience. We have created an abstraction layer that provides multi-language (e.g. Rego and CEL), multi-target policy enforcement to allow for portable policies and coexistence of numerous policy implementations.

The [Constraint Framework](https://github.com/open-policy-agent/frameworks/tree/master/constraint) is the library that underlies Gatekeeper. It provides the execution flow Gatekeeper uses to render a decision to the API server. It also provides abstractions that allow us to define constraint templates and constraints: Engine, Enforcement Points, and Targets.

Together with Gatekeeper and [gator CLI](gator.md), you can get admission, audit, and shift left validations for policies written in both CEL and Rego policy languages, even for clusters that do not support Validating Admission Policy feature yet. For simple policies, you may want admission requests to be handled by the K8s built-in Validating Admission Controller (only supports CEL) instead of the Gatekeeper admission webhook.

In summary, these are potential options when running Gatekeeper:

| Policy Language(s)    | Enforcement Point  |
| ------------------ | ------------------ |
| CEL, Rego          | Gatekeeper validating webhook |
| CEL, Rego          | Gatekeeper Audit   |
| CEL, Rego          | Gator CLI          |
| CEL                | K8s built-in Validating Admission Controller (aka ValidatingAdmissionPolicy) |
| Rego               | Gatekeeper validating webhook (referential policies, external data) |
| Rego               | Gatekeeper Audit (referential policies, external data) |
| Rego               | Gator CLI (referential policies) |

Find out more about different [enforcement points](enforcement-points.md)

## Pre-requisites

- Requires minimum Gatekeeper v3.17.0 (Please refer to the v3.16.0 docs as flags have changed)
- Requires minimum Kubernetes v1.30, when the Kubernetes `Validating Admission Policy` feature GAed
- [optional] Kubernetes version v1.29, need to enable Kubernetes feature gate and runtime config as shown below: 

    ```yaml
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    featureGates:
      ValidatingAdmissionPolicy: true
    runtimeConfig:
      admissionregistration.k8s.io/v1beta1: true
    ```

## Get started

## Policy updates to add VAP CEL
To see how it works, check out this [demo](https://github.com/open-policy-agent/gatekeeper/tree/master/demo/k8s-validating-admission-policy)

Example `K8sRequiredLabels` constraint template using the `K8sNativeValidation` engine and VAP CEL expressions that requires resources to contain specified labels with values matching provided regular expressions. A similar policy written in Rego can be seen [here](https://open-policy-agent.github.io/gatekeeper-library/website/validation/requiredlabels)

```yaml
apiVersion: templates.gatekeeper.sh/v1
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
          type: object
          properties:
            message:
              type: string
            labels:
              type: array
              items:
                type: object
                properties:
                  key:
                    type: string
                  allowedRegex:
                    type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: K8sNativeValidation
          source:
            validations:
            - expression: '[object, oldObject].exists(obj, obj != null && has(obj.metadata) && variables.params.labels.all(entry, has(obj.metadata.labels) && entry.key in obj.metadata.labels))'
              messageExpression: '"missing required label, requires all of: " + variables.params.labels.map(entry, entry.key).join(", ")'
            - expression: '[object, oldObject].exists(obj, obj != null && !variables.params.labels.exists(entry, has(obj.metadata.labels) && entry.key in obj.metadata.labels && !string(obj.metadata.labels[entry.key]).matches(string(entry.allowedRegex))))'
              message: "regex mismatch"
        rego: |
          ...
```

With this new engine and source added to the constraint template, now Gatekeeper webhook, audit, and shift-left can validate resources with these new VAP CEL-based rules.

## Policy updates to generate Validating Admission Policy and Binding resources

For some policies, you may want admission requests to be handled by the K8s Validating Admission Controller instead of the Gatekeeper admission webhook.

The K8s Validating Admission Controller requires both the Validating Admission Policy (VAP) and Validating Admission Policy Binding (VAPB) resources to exist to enforce a policy. Gatekeeper can be configured to generate both of these resources. To generate VAP Bindings for all Constraints, ensure the Gatekeeper 
`--default-create-vap-binding-for-constraint` flag is set to `true`. To generate VAP as part of all Constraint Templates with the VAP CEL engine `K8sNativeValidation`, ensure the Gatekeeper `--default-create-vap-for-templates=true` flag is set to `true`. By default both flags are set to `false` while the feature is still in alpha.

To override the `--default-create-vap-for-templates` flag's behavior for a constraint template, set `generateVAP` to `true` explicitly under the K8sNativeValidation engine's `source` in the constraint template. 

```yaml
spec:
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: K8sNativeValidation
          source:
            generateVAP: true
            ...
```

To override the `--default-create-vap-binding-for-constraints` flag's behavior for a constraint, `spec.scopedEnforcementAction` can be used. Gatekeeper determines the intended enforcement actions for a given enforcement point by evaluating what is provided in `spec.scopedEnforcementActions` and `spec.enforcementAction: scoped` in the constraint.

The overall opt-in/opt-out behavior for constraint to generate Validating Admission Policy Binding (VAPB) is as below:

Constraint with `enforcementAction: scoped`:

| `vap.k8s.io` in constraint with `spec.scopedEnforcementActions` | generate VAPB |
|----------|----------|
| Not included | Do not generate VAPB |
| Included | Generate VAPB |

Constraint without `enforcementAction: scoped`:

| `--default-create-vap-binding-for-constraints` | generate VAPB |
|----------|----------|
| false | Do not generate VAPB |
| true | Generate VAPB |

:::note
VAP will only get generated for templates with VAP CEL Engine. VAPB will only get generated for constraints that belong to templates with VAP CEL engine.
:::

:::tip
In the event K8s Validating Admission Controller fails open, Gatekeeper admission webhook can act as a backup when included in constraint.
:::

Validating Admission Policy Binding for the below constraint will always get generated, assuming the constraint belongs to a template with VAP CEL engine.

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sAllowedRepos
metadata:
  name: prod-repo-is-openpolicyagent
spec:
...
  enforcementAction: scoped
  scopedEnforcementActions:
  - action: deny
    enforcementPoints:
    - name: "vap.k8s.io"
    - name: "validation.gatekeeper.sh"
...
```
