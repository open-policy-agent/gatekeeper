---
id: expansion
title: Validating Workload Resources using `ExapansionTemplate`s
---

`Feature State:` Gatekeeper version v3.10+ (alpha)

> ❗This feature is in _alpha_ stage, and is not enabled by default. To
> enable, set the `enable-generator-resource-expansion` flag.

## Motivation

A workload resource is a resource that creates other resources, such as a
[Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) or [Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/). Gatekeeper can be configured to reject workload resources
that create a resource that violates a constraint. 

## `ExpansionTemplate`s explained

An `ExpansionTemplate` is a custom resource that Gatekeeper will use to simulate a workload resource expansion. Gatekeeper will create temporary, fake resources and validate the constraints against them. We refer to these resources that Gatekeeper creates for validation purposes as `expanded resources`, but  the terms `mock resources`, `simulated resources`, `resulting resources`, and `resultant resources` all refer to the same notion.

The `ExpansionTemplate` custom resource specifies:

- Which workload resource(s) should be simulated, specified by their GVK
- The GVK of the resulting resources
- The "source", a field to use as the blueprint for the resulting resource. The template
  source is a field on the parent resource which will be used as the base (TODO: define) for
  expanding. For example, in a case where a
  `Deployment` expands into a `Pod`, `spec.template` would typically be the
  source.
- Optionally, an `enforcementAction` override can be used when validating resultant
  resources. If this field is set, any violations against the resulting resource
  will use this enforcement action. If an enforcement action is not specified by
  the `ExpansionTemplate`, the enforcement action set by the Constraint in
  violation will be used.

Here is an example of a `ExpansionTemplate` that specifies that `DaemonSet`,
`Deployment`, `Job`, `ReplicaSet`, `ReplicationController`, and `StatefulSet`
 resources should be expanded into a `Pod`.

```yaml
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
    - groups: ["apps"]
      kinds: ["DaemonSet", "Deployment", "ReplicaSet", "StatefulSet"]
      versions: ["v1"]
    - groups: [""]
      kinds: ["ReplicationController"]
      versions: ["v1"]
    - groups: ["batch"]
      kinds: ["Job"]
      versions: ["v1"]
  templateSource: "spec.template"
  enforcementAction: "warn" # This will overwrite all constraint enforcement actions for the GVKs below that result from the GVKs above.
  generatedGVK:
    kind: "Pod"
    group: ""
    version: "v1"
```

With this `ExpansionTemplate`, any constraints that are configured to target
`Pods` will also be evaluated on the temporary, fake pods that Gatekeeper creates when a `Deployment` or `ReplicaSet` is
being reviewed. Any violations created against these resulting `Pod`s, and only these resulting `Pod`s, will have their
enforcement action set to `warn`, regardless of the enforcement actions
specified by the Constraint in violation.

If, for any reason, you want to exempt simulated resources from a specific
constraint, look at the [Match Source](#match-source) section below.

### Limitations

#### Sidecars and Mutators

It may not always be possible to build an accurate representation of a
mock resource by looking at the workload resource alone. For example, suppose a
cluster is using [Istio](https://istio.io/), which will inject a sidecar container on specific
resources. This sidecar configuration is not specified in the config of the
workload resource (i.e. Deployment), but rather injected by Istio's webhook. In
order to accurately represent mock resources modified by controllers or
webhooks, Gatekeeper leverages its
[Mutations](mutation.md)
feature to allow mock resources to be manipulated into their desired form. In
the Istio example, `Assign` and `ModifySet` mutators could be configured to
mimic Istio sidecar injection. For further details on mutating mock resources
see the [Match Source](#match-source) section below, or to see a working example,
see the [Example](#example) section.

#### Enforcement Points

Any resources configured for expansion will be expanded by both the validating
webhook and
[Audit](audit.md). This
feature will only be enabled if a user creates an `ExpansionTemplate` that
targets any resources that exist on the cluster.

Note that the accuracy of enforcement depends on how well the mock resource
resembles the real thing. Mutations can help with this, but 100% accuracy is
impossible because not all fields can be predicted. For instance, Deployments
create pods with random names. Inaccurate mocks may lead to over- or under-
enforcement. In the case of under-enforcement, the resultant pod should still be
rejected. Finally, non-state-based policies (those that rely on transient
metadata such as requesting user or time of creation) cannot be enforced
accurately. This is because such metadata would necessarily be different when
creating the resultant resource. For example, a Deployment is created using the
requesting user's account, but the pod creation request comes from the service
account of the Deployment controller.

## Configuring Expansion

Expansion behavior is configured through the `ExpansionTemplate` custom
resource. Optionally, users can create `Mutation` custom resources to customize
how resources are expanded. Mutators with the `source: Generated` field will
only be applied when expanding workload resources, and will not mutate real
resources on the cluster. If the `source` field is not set, the `Mutation` will
apply to both expanded resources and real resources on the cluster.

Users can test their expansion configuration using the
[`gator expand` CLI](gator.md#the-gator-expand-subcommand)
.

#### Match Source

The `source` field on the `match` API, present in the Mutation
and `Constraint` kinds, specifies if the config should match Generated (
i.e. fake) resources, Original resources, or both. The `source` field is
an `enum` which accepts the following values:

- `Generated` – the config will only apply to expanded resources, **and will not
  apply to any real resources on the cluster**
- `Original` – the config will only apply to Original resources, and will not
  affect expanded resources
- `All` – the config will apply to both `Generated` and `Original` resources.
  This is the default value.

For example, suppose a cluster's `ReplicaSet` controller adds a default value
for `fooField` when creating Pods that cannot reasonably be added to the
`ReplicaSet`'s `spec.template`. If a constraint relies on these default values,
a user could create a Mutation custom resource that modifies expanded resources,
like so:

```yaml
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

Similarly, `Constraints` can be configured to only target fake resources by
setting the `Constraint`'s `spec.match.source` field to `Generated`. This can
also be used to define different enforcement actions for expanded resources and
original resources.

## Mutating Example

Suppose a cluster is using Istio, and has a policy configured to ensure
specified Pods have an Istio sidecar. To validate Deployments that would create
Pods which Istio will inject a sidecar into, we need to use mutators to mimic
the sidecar injection.

What follows is an example of:

- an  `ExpansionTemplate` configured to expand `Deployments` into `Pods`
- an `Assign` mutator to add the Istio sidecar container to `Pods`
- a `ModifySet` mutator to add the `proxy` and `sidecar` args
- an inbound `Deployment`, and the resulting `Pod`

**Note that the Mutators set the `source: Generated` field, which will cause
them to only be applied when expanding resources specified
by `ExpansionTemplates`. These Mutators will not affect any real resources on
the cluster.**

```yaml
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
    - groups: ["apps"]
      kinds: ["Deployment"]
      versions: ["v1"]
  templateSource: "spec.template"
  generatedGVK:
    kind: "Pod"
    group: ""
    version: "v1"
---
apiVersion: mutations.gatekeeper.sh/v1beta1
kind: Assign
metadata:
  name: add-sidecar
spec:
  applyTo:
    - groups: [""]
      kinds: ["Pod"]
      versions: ["v1"]
  match:
    scope: Namespaced
    source: All
    kinds:
      - apiGroups: ["*"]
        kinds: ["Pod"]
  location: "spec.containers[name:istio-proxy]"
  parameters:
    assign:
      value:
        name: "istio-proxy"
        imagePullPolicy: IfNotPresent
        image: docker.io/istio/proxyv2:1.15.0
        ports:
          - containerPort: 15090
            name: http-envoy-prom
            protocol: TCP
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
              - ALL
---
apiVersion: mutations.gatekeeper.sh/v1beta1
kind: ModifySet
metadata:
  name: add-istio-args
spec:
  applyTo:
    - groups: [""]
      kinds: ["Pod"]
      versions: ["v1"]
  match:
    source: All
  location: "spec.containers[name:istio-proxy].args"
  parameters:
    operation: merge
    values:
      fromList:
        - proxy
        - sidecar
---
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

```yaml
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
    name: nginx
    ports:
    - containerPort: 80
  - args:
    - proxy
    - sidecar
    image: docker.io/istio/proxyv2:1.15.0
    imagePullPolicy: IfNotPresent
    name: istio-proxy
    ports:
    - containerPort: 15090
      name: http-envoy-prom
      protocol: TCP
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
```


