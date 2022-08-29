---
id: expansion
title: Early Rejection of Generator Resources
---

> 🚧 This feature is in _alpha_ stage, and is not enabled by default. To
> enable, set the "enable-generator-resource-expansion" flag.

Gatekeeper can be configured to reject generator resources that might create a
resource that violates a policy. For example, one could configure Gatekeeper to
immediately reject deployments that would create a `Pod` that violates a
constraint instead of merely rejecting the Pods. To achieve this, Gatekeeper
creates a "mock resource" for the `Pod`, runs validation on it, and aggregates
the mock resource's violations onto the parent resource (the `Deployment` in
this example).

To use this functionality, first specify which resources should be "expanded" by
creating 1 or more `TemplateExpansion` custom resources. Then, in order for
Gatekeeper to accurately create these "mock resources" in a way that mirrors the
real Kubernetes controllers, users can define any number of
[Mutations](https://open-policy-agent.github.io/gatekeeper/website/docs/mutation)
on the expanded resource to manipulate them into the desired form.

Any resources configured for expansion will be expanded for both validation and
[Audit](https://open-policy-agent.github.io/gatekeeper/website/docs/audit). This
feature will only be enabled if a user creates a `TemplateExpansion`
that targets some resource that exists on the cluster.

Note that the accuracy of enforcement depends on how well the mock resource
resembles the real thing. Mutations can help with this, but 100% accuracy is
impossible because not all fields can be predicted. For instance, deployments
create pods with random names. Inaccurate mocks may lead to over or under
enforcement. In the case of under enforcement, the resultant pod should still be
rejected. Finally, non-state-based policies (those that rely on transient
metadata such as requesting user or time of creation) cannot be enforced
accurately. This is because such metadata would necessarily be different when
creating the resultant resource. For example, a deployment is created using the
requesting user's account, but the pod creation request comes from the service
account of the deployment controller.

If, for any reason, you want to exempt expanded resources from a specific
constraint, look at the [Match Source](#match-source) section below.

## Configuring Expansion

Expansion behavior is configured through an `ExpansionTemplate` CR. Optionally,
users can create `Mutation` CRs to customize how resources are expanded.

Users can test their expansion configuration using the
[`gator expand` CLI](https://open-policy-agent.github.io/gatekeeper/website/docs/gator)
.

#### ExpansionTemplate

The `ExpansionTemplate` CR specifies:

- Which resource(s) should be expanded, specified by their GVK
- The GVK of the resultant resources
- Which field to use as the "source" for the resultant resource. The source is a
  field on the parent resource which will be used as the base for expanding it
  before any mutators are applied. For example, in a case where a `Deployment`
  expands into a `Pod`, `spec.template` would typically be the source.

Here is an example of a `ExpansionTemplate` that specifies that `DeamonSet`,
`Deployment`, `Job`, `ReplicaSet`, `ReplicationController`, and `StatefulSet`
should be expanded into a `Pod`.

```
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
    - groups: ["apps"]
      kinds: ["DeamonSet", "Deployment", "Job", "ReplicaSet", "ReplicationController", "StatefulSet"]
      versions: ["v1"]
  templateSource: "spec.template"
  generatedGVK:
    kind: "Pod"
    group: ""
    version: "v1"
```

With this `ExpansionTemplate`, any constraints that are configured to
target `Pods` will be evaluated on the "mock Pods" when a `Deployment`
/`ReplicaSet` is being reviewed.

#### Match Source

The `source` field on the `match` API, present in the Mutation
and `ConstraintTemplate` kinds, specifies if the config should match Generated (
i.e. expanded) resources, Original resources, or both. The `source` field is
an `enum` which accepts the following values:

- `Generated` – the config will only apply to expanded resources
- `Original` – the config will only apply to Original resources, and will not
  effect expanded resources
- `All` – the config will apply to both `Generated` and `Original` resources.
  This is the default value.

For example, suppose a cluster's `ReplicaSet` controller adds a default value
for `fooField` when creating Pods that cannot reasonably be added to
the `ReplicaSet`'s `spec.template`. If a constraint relies on these default
values, a user could create a Mutation CR that modifies expanded resources, like
so:

```
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assign-foo-field
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  location: "spec.containers[name: *].fooField"
  parameters:
    assign:
      value: "Bar"
  match:
    source: "Generated"
    scope: Cluster
    kinds:
      - apiGroups: []
        kinds: []
```

Similarly, `Constraints` can be configured to only target expanded resources by
setting the `Constraint`'s `spec.match.source` field to `Generated`. This can
also be used to define different enforcement actions for expanded resources and
original resources.

## Example

What follows is an example of:

- an  `ExpansionTemplate` configured to expand `Deployments` into `Pods`
- an `Assign` mutator to set the `imagePullPolicy` on expanded `Pods`
- an inbound `Deployment`, and the resulting `Pod`

```
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
  - groups: [ "apps" ]
    kinds: [ "Deployment" ]
    versions: [ "v1" ]
  templateSource: "spec.template"
  generatedGVK:
    kind: "Pod"
    group: ""
    version: "v1"
—--
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: always-pull-image
spec:
  applyTo:
  - groups: [ "" ]
    kinds: [ "Pod" ]
    versions: [ "v1" ]
  location: "spec.containers[name: *].imagePullPolicy"
  parameters:
    assign:
      value: "Always"
  match:
    source: "Generated"
    scope: Namespaced
    kinds:
    - apiGroups: [ ]
      kinds: [ ]
—--
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  labels:
    app: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
        args:
        - "/bin/sh"
```

When expanded, the above configs will produce the following `Pod`:

```
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: nginx
spec:
  containers:
  - args:
    - /bin/sh
    image: nginx:1.14.2
    imagePullPolicy: Always
    name: nginx
    ports:
    - containerPort: 80
```


