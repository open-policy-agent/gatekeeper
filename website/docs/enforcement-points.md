---
id: enforcement-points
title: Enforcement points in Gatekeeper
---

## Understanding Enforcement Points

An enforcement point defines the location where enforcement happens. Below are the different enforcement points available in Gatekeeper:

- `validation.gatekeeper.sh` indicates that enforcement should be carried out by Gatekeeper's validating webhook for a constraint. Supports templates with CEL and Rego.
- `gator.gatekeeper.sh` indicates that enforcement should be carried out in shift-left via [gator-cli](gator.md) for a constraint. Supports templates with CEL and Rego.
- `audit.gatekeeper.sh` indicates that on-cluster resources should be audited and violations should be reported for the resources that are in violation of constraint. Supports templates with CEL and Rego.
- `vap.k8s.io` indicates that enforcement should be carried out by Validating Admission Policy for a constraint. Supports templates with CEL.

### How to use different enforcement points in constraint

By default, a constraint will be enforced at all enforcement points with common enforcement action defined in `spec.enforcementAction`. However, you can choose to enforce a constraint at specific enforcement points with different actions using `enforcementAction: scoped` and `spec.scopedEnforcementActions`. Below are examples and use cases that utilize different enforcement actions for different enforcement points.

:::note
`spec.enforcementAction: scoped` is needed to customize specific enforcement point/enforcement action behavior. If `spec.enforcementAction: scoped` is not provided, `spec.scopedEnforcementActions` is ignored and the provided `enforcementAction` will be applied across all enforcement points.
:::

###### Deny in shift-left and warn at admission

You are trying out a new constraint template, and you want to deny violating resources in shift-left testing, but do not want to block any resources admitted to clusters to reduce impact for faulty rejections. You may want to use `deny` action for the `gator.gatekeeper.sh` shift-left enforcement point and `warn` for `the validation.gatekeepet.sh` admission webhook enforcement point. The below constraint satisfies this use case.

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sAllowedRepos
metadata:
  name: prod-repo-is-openpolicyagent
spec:
...
  enforcementAction: scoped
  scopedEnforcementActions:
  - action: warn
    enforcementPoints:
    - name: "validation.gatekeeper.sh"
  - action: deny
    enforcementPoints:
    - name: "gator.gatekeeper.sh"
...
```

> **Note**: The audit enforcement point is not included unless explicitly added to scopedEnforcementActions.enforcementPoints or if scopedEnforcementActions.enforcementPoints is set to "*".

###### Only audit

You are depending on external-data or referential policies for validating resources. These type of validation may be latency sensitive and may take longer to evaluate. To avoid such situation you may want to only use `audit.gatekeeper.sh` enforcement point to not face any delay at admission time, but still get the information about violating resources from Gatekeeper's audit operation. Here is the constraint for only using `audit.gatekeeper.sh` enforcement point.

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
    - name: "audit.gatekeeper.sh"
...
```

###### Enforcing through Validating Admission Policy and using Gatekeeper as fall-back validation mechanism

You want to utilize in-tree Validating Admission Policy for faster turn around time. But you want to make sure that in case Validating Admission Policy fails-open, Gatekeeper blocks faulty resources from being created. Here is how you can achieve the same.

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

Please refer to [VAP/VAPB generation behavior](validating-admission-policy.md#policy-updates-to-generate-validating-admission-policy-resources).

###### Enforcing through Validating Admission Policy and using audit from Gatekeeper

You want to utilize in-tree Validating Admission Policy for faster turn around time and only want to use audit operation from Gatekeeper to get information about violation resources on-cluster. Here is the constraint that users `vap.k8s.io` and `audit.gatekeeper.sh` enforcement points.

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
    - name: "audit.gatekeeper.sh"
...
```
