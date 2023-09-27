---
id: validating-admission-policy
title: Integration with Kubernetes Validating Admission Policy
---

`Feature State`: Gatekeeper version v3.13+ (pre-alpha)

> ❗ This feature is pre-alpha, subject to change (feedback is welcome!). It is disabled by default. To enable the feature,
> set the `experimental-enable-k8s-native-validation` flag to true and use the [development build of Gatekeeper](https://open-policy-agent.github.io/gatekeeper/website/docs/install/#deploying-a-release-using-development-image). Do not use this feature with `validate-template-rego` flag enabled, as the policies with cel would get rejected with rego compilation error.

## Description

This feature allows Gatekeeper to integrate with Kubernetes Validating Admission Policy based on [Common Expression Language (CEL)](https://github.com/google/cel-spec), a declarative, in-process admission control alternative to validating admission webhooks.

## Motivations

The Validating Admission Policy feature (disabled by default) was introduced as an alpha feature to Kubernetes v1.26, beta in v1.28. Some of the benefits include:
- in-tree/native in-process
- reduce admission request latency
- improve reliability and availability
- able to fail closed without impacting availability
- avoid the operational burden of webhooks

To reduce policy fragmentation and simplify the user experience by standardizing the policy experience. We have created an abstraction layer that provides multi-language (e.g. Rego and CEL), multi-target policy enforcement to allow for portable policies and coexistence of numerous policy implementations.

The [Constraint Framework](https://github.com/open-policy-agent/frameworks/tree/master/constraint) is the library that underlies Gatekeeper. It provides the execution flow Gatekeeper uses to render a decision to the API server. It also provides abstractions that allow us to define constraint templates and constraints: Engine, Enforcement Points, and Targets.

Together with Gatekeeper and [gator CLI](gator.md), you can get admission, audit, and shift left validations for both CEL-based Validating Admission Policy and OPA Rego policies, even for clusters that do not support Validating Admission Policy feature yet.

## Example Constraint Template
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
            - expression: "variables.params.labels.all(entry, has(object.metadata.labels) && entry.key in object.metadata.labels)"
              messageExpression: '"missing required label, requires all of: " + variables.params.labels.map(entry, entry.key).join(", ")'
            - expression: "!variables.params.labels.exists(entry, has(object.metadata.labels) && entry.key in object.metadata.labels && !string(object.metadata.labels[entry.key]).matches(string(entry.allowedRegex)))"
              message: "regex mismatch"
```
