> [!WARNING]
> This is a demo of an alpha feature and is subject to change.
This demo shows two new capabilities:
1. Together with Gatekeeper and gator CLI, you can get admission, audit, and shift left validations for both CEL-based Validating Admission Policy and OPA Rego policies, even for clusters that do not support Kubernetes Validating Admission Policy feature yet.
1. Gatekeeper can be enabled to generate Kubernetes Validating Admission Policy resources such that admission validation can be handled by [Kubernetes's in-process Validating Admission Policy Controller](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/) instead of the Gatekeeper admission webhook. In the event the Validating Admission Policy Controller fails open, then Gatekeeper admission webhook can act as a fallback. This requires clusters with the Kubernetes Validating Admission Policy feature enabled.

## Pre-requisites

- Requires minimum Gatekeeper v3.16.0-beta.2
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

## Policy updates to add CEL

Under `target`, add a `K8sNativeValidation` engine and the source code:

```yaml
- target: admission.k8s.gatekeeper.sh
  code:
  - engine: K8sNativeValidation
    source:
      validations:
      - expression: '...'
        messageExpression: 'some CEL string'
      - expression: '...'
        message: "some fallback message"
  rego: |
    ...
```
With this new engine and source added to the constraint template, now Gatekeeper webhook, audit, and shift-left can validate resources with these new CEL-based rules. 

## Policy updates to generate Validating Admission Policy resources

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

## Demo

<img width= "900" height="500" src="demo.gif" alt="cel demo">