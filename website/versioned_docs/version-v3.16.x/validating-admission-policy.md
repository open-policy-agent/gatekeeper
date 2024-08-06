---
id: validating-admission-policy
title: Integration with Kubernetes Validating Admission Policy
---

`Feature State`: Gatekeeper version v3.16 (alpha)

> ❗ This feature is alpha, subject to change (feedback is welcome!). It is disabled by default.

## Description

This feature allows Gatekeeper to integrate with Kubernetes Validating Admission Policy based on [Common Expression Language (CEL)](https://github.com/google/cel-spec), a declarative, in-process admission control alternative to validating admission webhooks.

## Motivations

The Validating Admission Policy feature was introduced as an alpha feature to Kubernetes v1.26, beta in v1.28 (disabled by default), GA in v1.30 (enabled by default). Some of the benefits include:
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

## Pre-requisites

- Requires minimum Gatekeeper v3.16.0
- Requires minimum Kubernetes v1.30, when the `Validating Admission Policy` feature GAed
- [optional] Kubernetes version v1.29, need to enable feature gate and runtime config as shown below: 

    ```yaml
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    featureGates:
      ValidatingAdmissionPolicy: true
    runtimeConfig:
      admissionregistration.k8s.io/v1beta1: true
    ```
- Set `--experimental-enable-k8s-native-validation` in Gatekeeper deployments, or `enableK8sNativeValidation=true` if using Helm.

## Get started

### Option 1: Install with Helm
Update the `enableK8sNativeValidation` parameter in values.yaml or set during deployment
- Enable the K8s Native Validating driver to allow users to create CEL-based rules in addition to the OPA driver and rego rules (alpha feature). Default is `false`
```shell
helm install gatekeeper/gatekeeper --name-template=gatekeeper --namespace gatekeeper-system --create-namespace \
    --set enableK8sNativeValidation=true
```

### Option 2: Install with Gatekeeper deployment
Edit the applicable deployments (`controller-manager` and `audit`), and update the following commandline flags:
- Set `--experimental-enable-k8s-native-validation=true`

## Policy updates to add CEL
To see how it works, check out this [demo](https://github.com/open-policy-agent/gatekeeper/tree/master/demo/k8s-validating-admission-policy)

Example `K8sRequiredLabels` constraint template using the `K8sNativeValidation` engine and CEL expressions that requires resources to contain specified labels with values matching provided regular expressions. A similar policy written in Rego can be seen [here](https://open-policy-agent.github.io/gatekeeper-library/website/validation/requiredlabels)

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
With this new engine and source added to the constraint template, now Gatekeeper webhook, audit, and shift-left can validate resources with these new CEL-based rules. 

## Policy updates to generate Validating Admission Policy resources

For some policies, you may want admission requests to be handled by the K8s Validating Admission Controller instead of the Gatekeeper admission webhook. By default, Gatekeeper is configured to generate K8s Validating Admission Policy and Validating Admission Policy Binding respectively for constraint template & constraint if the `gatekeeper.sh/use-vap: yes` label is added on the resources. Default value for this feature flag is `--vap-enforcement=GATEKEEPER_DEFAULT`.

Allowed values are:

- `NONE`: do not generate
- `GATEKEEPER_DEFAULT`: do not generate unless label `gatekeeper.sh/use-vap: yes` is added to policy explicitly
- `VAP_DEFAULT`: generate unless label `gatekeeper.sh/use-vap: no` is added to policy explicitly

To explicitly enable Gatekeeper to generate K8s Validating Admission Policy resources at the constraint template level, add the following label to the constraint template resource:

```yaml
labels:
  "gatekeeper.sh/use-vap": "yes"
```

By default, constraints will inherit the same behavior as the constraint template. However this behavior can be overriden by adding the following label to the constraint resource:

```yaml
labels:
  "gatekeeper.sh/use-vap": "no"
```

> [!TIP]
> In the event K8s Validating Admission Controller fails open, then Gatekeeper admission webhook can act as a backup.
