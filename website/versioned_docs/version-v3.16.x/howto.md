---
id: howto
title: How to use Gatekeeper
---

Gatekeeper uses the [OPA Constraint Framework](https://github.com/open-policy-agent/frameworks/tree/master/constraint) to describe and enforce policy. Look there for more detailed information on their semantics and advanced usage.

## Constraint Templates

Before you can define a constraint, you must first define a [`ConstraintTemplate`](constrainttemplates.md), which describes both the [Rego](https://www.openpolicyagent.org/docs/latest/#rego) that enforces the constraint and the schema of the constraint. The schema of the constraint allows an admin to fine-tune the behavior of a constraint, much like arguments to a function.

Here is an example constraint template that requires all labels described by the constraint to be present:

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

You can install this ConstraintTemplate with the following command:

```sh
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/demo/basic/templates/k8srequiredlabels_template.yaml
```

## Constraints

Constraints are then used to inform Gatekeeper that the admin wants a ConstraintTemplate to be enforced, and how. This constraint uses the `K8sRequiredLabels` constraint template above to make sure the `gatekeeper` label is defined on all namespaces:

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
    labels: ["gatekeeper"]
```

You can install this Constraint with the following command:

```sh
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/demo/basic/constraints/all_ns_must_have_gatekeeper.yaml
```
### The match field

The `match` field defines which resources the constraint will be applied to. It supports the following types of matchers:

   * `kinds` accepts a list of objects with `apiGroups` and `kinds` fields that list the groups/kinds of objects to which the constraint will apply. If multiple groups/kinds objects are specified, only one match is needed for the resource to be in scope.
   * `scope` determines if cluster-scoped and/or namespaced-scoped resources are matched.  Accepts `*`, `Cluster`, or `Namespaced`. (defaults to `*`)
   * `namespaces` is a list of namespace names. If defined, a constraint only applies to resources in a listed namespace.  Namespaces also supports a prefix-based glob.  For example, `namespaces: [kube-*]` matches both `kube-system` and `kube-public`.
   * `excludedNamespaces` is a list of namespace names. If defined, a constraint only applies to resources not in a listed namespace. ExcludedNamespaces also supports a prefix-based glob.  For example, `excludedNamespaces: [kube-*]` matches both `kube-system` and `kube-public`.
   * `labelSelector` is the combination of two optional fields: `matchLabels` and `matchExpressions`.  These two fields provide different methods of selecting or excluding k8s objects based on the label keys and values included in object metadata.  All selection expressions are ANDed to determine if an object meets the cumulative requirements of the selector.
   * `namespaceSelector` is a label selector against an object's containing namespace or the object itself, if the object is a namespace.
   * `name` is the name of a Kubernetes object.  If defined, it matches against objects with the specified name.  Name also supports a prefix-based glob.  For example, `name: pod-*` matches both `pod-a` and `pod-b`.

Note that if multiple matchers are specified, a resource must satisfy each top-level matcher (`kinds`, `namespaces`, etc.) to be in scope. Each top-level matcher has its own semantics for what qualifies as a match. An empty matcher, a undefined `match` field, is deemed to be inclusive (matches everything). Also understand `namespaces`, `excludedNamespaces`, and `namespaceSelector` will match on cluster scoped resources which are not namespaced. To avoid this adjust the `scope` to `Namespaced`.

### The parameters field

The `parameters` field describes the intent of a constraint. It can be referenced as `input.parameters` by the ConstraintTemplate's Rego source code. Gatekeeper populates `input.parameters` with values passed into the `parameters` field in the Constraint.

Example:
```yaml
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
The schema for the input Constraint `parameters` is defined in the ConstraintTemplate. The API server will reject a Constraint with an incorrect parameters field if the data types do not match.

Example:
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

### The enforcementAction field

The `enforcementAction` field defines the action for handling Constraint violations. By default, `enforcementAction` is set to `deny` as the default behavior is to deny admission requests with any violation. Other supported enforcementActions include `dryrun` and `warn`. Refer to [Handling Constraint Violations](violations.md) for more details.

### Listing constraints
You can list all constraints in a cluster with the following command:

```sh
kubectl get constraints
```

### Input Review
You can view information on the `input.review` object that Gatekeeper takes as input [here](./input.md)

