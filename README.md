# Gatekeeper

[![Build Status](https://travis-ci.org/open-policy-agent/gatekeeper.svg?branch=master)](https://travis-ci.org/open-policy-agent/gatekeeper) [![Docker Repository on Quay](https://quay.io/repository/open-policy-agent/gatekeeper/status "Docker Repository on Quay")](https://quay.io/repository/open-policy-agent/gatekeeper)

## Want to help?
Join us to help define the direction and implementation of this project!

- Join the [`#kubernetes-policy`](https://openpolicyagent.slack.com/messages/CDTN970AX)
  channel on [OPA Slack](https://slack.openpolicyagent.org/).

- Join [weekly meetings](https://docs.google.com/document/d/1A1-Q-1OMw3QODs1wT6eqfLTagcGmgzAJAjJihiO3T48/edit)
  to discuss development, issues, use cases, etc.

- Use [GitHub Issues](https://github.com/open-policy-agent/gatekeeper/issues)
  to file bugs, request features, or ask questions asynchronously.

## How is Gatekeeper different from OPA?
Compared to using [OPA with its sidecar kube-mgmt](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html) (aka Gatekeeper v1.0), Gatekeeper introduces the following functionality:

   * An extensible, parameterized policy library
   * Native Kubernetes CRDs for instantiating the policy library (aka "constraints")
   * Native Kubernetes CRDs for extending the policy library (aka "constraint templates")
   * Audit functionality

## Goals

Every organization has policies. Some are essential to meet governance and legal requirements. Others help ensure adherance to best practices and institutional conventions. Attempting to ensure compliance manually would be error-prone and frustrating. Automating policy enforcement ensures consistency, lowers development latency through immediate feedback, and helps with agility by allowing developers to operate independently without sacrificing compliance.

Kubernetes allows decoupling policy decisions from the inner workings of the API Server by means of [admission controller webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/), which are executed whenever a resource is created, updated or deleted. Gatekeeper is a validating (mutating TBA) webhook that enforces CRD-based policies executed by [Open Policy Agent](https://github.com/open-policy-agent/opa), a policy engine for Cloud Native environments hosted by CNCF as an incubation-level project.

In addition to the `admission` scenario, Gatekeeper's audit functionality allows administrators to see what resources are currently violating any given policy.

Finally, Gatekeeper's engine is designed to be portable, allowing administrators to detect and reject non-compliant commits to an infrastructure-as-code system's source-of-truth, further strengthening compliance efforts and preventing bad state from slowing down the organization.

## Installation Instructions

### Installation

#### Prerequisites

##### Minimum Kubernetes Version

**To use Gatekeeper, you should have a minimum Kubernetes version of 1.14, which adds
webhook timeouts.**

You can install Gatekeeper in earlier versions of Kubernetes either by
removing incompatible fields from the manifest or by setting `--validate=false`
when applying the manifest. Be warned that, without timeouts on the webhook, your
API Server could timeout when Gatekeeper is down. Kubernetes 1.14 fixes this issue.

##### RBAC Permissions

For either installation method, make sure you have cluster admin permissions:

```sh
  kubectl create clusterrolebinding cluster-admin-binding \
    --clusterrole cluster-admin \
    --user <YOUR USER NAME>
```

#### Deploying a Release using Prebuilt Image

If you want to deploy a released version of Gatekeeper in your cluster with a prebuilt image, then you can run the following command:

```sh
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/deploy/gatekeeper.yaml
```

#### Deploying HEAD Using make

Currently the most reliable way of installing Gatekeeper is to build and install from HEAD:

   * Make sure that:
       * [Kubebuilder](https://github.com/kubernetes-sigs/kubebuilder#getting-started) and [Kustomize](https://github.com/kubernetes-sigs/kustomize/blob/master/docs/INSTALL.md) are installed.
       * Your kubectl context is set to the desired installation cluster.
       * You have a container registry you can write to that is readable by the target cluster.
   * Clone the Gatekeeper repository to your local system:
     ```sh
     git clone https://github.com/open-policy-agent/gatekeeper.git
     ```
   * `cd` to the repository directory.
   * Define your destination Docker image location:
      ```sh
      export DESTINATION_GATEKEEPER_DOCKER_IMAGE=<YOUR DESIRED DESTINATION DOCKER IMAGE> 
      ```
   * Build and push your Docker image: 
      ```sh
      make docker-build REPOSITORY="$DESTINATION_GATEKEEPER_DOCKER_IMAGE"
      make docker-push-release REPOSITORY="$DESTINATION_GATEKEEPER_DOCKER_IMAGE"
      ```
   * Finally, deploy: 
     ```sh
     make deploy REPOSITORY="$DESTINATION_GATEKEEPER_DOCKER_IMAGE"
     ```

#### Deploying via Helm ####

A basic Helm v2 template exists in `chart/gatekeeper-operator`. If you have Helm installed and Tiller initialized on your cluster you can deploy via 
```sh
helm install chart/gatekeeper-operator/
```

You can alter the variables in `chart/gatekeeper-operator/values.yaml` to customize your deployment. To regenerate the base template, run `make manifests`.

### Uninstallation

Before uninstalling Gatekeeper, be sure to clean up old `Constraints`, `ConstraintTemplates`, and
the `Config` resource in the `gatekeeper-system` namespace. This will make sure all finalizers
are removed by Gatekeeper. Otherwise the finalizers will need to be removed manually.

#### Before Uninstall, Clean Up Old Constraints

Currently the uninstall mechanism only removes the Gatekeeper system, it does not remove any `ConstraintTemplate`, `Constraint`, and `Config` resources that have been created by the user, nor does it remove their accompanying `CRDs`.

When Gatekeeper is running it is possible to remove unwanted constraints by:
   * Deleting all instances of the constraint resource
   * Deleting the `ConstraintTemplate` resource, which should automatically clean up the `CRD`
   * Deleting the `Config` resource removes finalizers on synced resources

#### Uninstall Gatekeeper

##### Using Prebuilt Image

If you used a prebuilt image to deploy Gatekeeper, then you can delete all the Gatekeeper components with the following command:

  ```sh
  kubectl delete -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/deploy/gatekeeper.yaml
  ```

##### Using make

If you used `make` to deploy, then run the following to uninstall Gatekeeper:

   * cd to the repository directory
   * run `make uninstall`

##### Using Helm

If you used `helm` to deploy, then run the following to uninstall Gatekeeper:
```sh
helm delete <release name> --purge
```

##### Manually Removing Constraints

If Gatekeeper is no longer running and there are extra constraints in the cluster, then the finalizers, CRDs and other artifacts must be removed manually:

   * Delete all instances of the constraint resource
   * Executing `kubectl patch  crd constrainttemplates.templates.gatekeeper.sh -p '{"metadata":{"finalizers":[]}}' --type=merge`. Note that this will remove all finalizers on every CRD. If this is not something you want to do, the finalizers must be removed individually.
   * Delete the `CRD` and `ConstraintTemplate` resources associated with the unwanted constraint.

## How to Use Gatekeeper

Gatekeeper uses the [OPA Constraint Framework](https://github.com/open-policy-agent/frameworks/tree/master/constraint) to describe and enforce policy. Look there for more detailed information on their semantics and advanced usage.

### Constraint Templates

Before you can define a constraint, you must first define a `ConstraintTemplate`, which describes both the [Rego](https://www.openpolicyagent.org/docs/latest/#rego) that enforces the constraint and the schema of the constraint. The schema of the constraint allows an admin to fine-tune the behavior of a constraint, much like arguments to a function.

Here is an example constraint template that requires all labels described by the constraint to be present:

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
        listKind: K8sRequiredLabelsList
        plural: k8srequiredlabels
        singular: k8srequiredlabels
      validation:
        # Schema for the `parameters` field
        openAPIV3Schema:
          properties:
            labels:
              type: array
              items: string
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

### Constraints

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

Note the `match` field, which defines the scope of objects to which a given constraint will be applied. It supports the following matchers:

   * `kinds` accepts a list of objects with `apiGroups` and `kinds` fields that list the groups/kinds of objects to which the constraint will apply. If multiple groups/kinds objects are specified, only one match is needed for the resource to be in scope.
   * `namespaces` is a list of namespace names. If defined, a constraint will only apply to resources in a listed namespace.
   * `labelSelector` is a standard Kubernetes label selector.
   * `namespaceSelector` is a standard Kubernetes namespace selector. If defined, make sure to add `Namespaces` to your `configs.config.gatekeeper.sh` object to ensure namespaces are synced into OPA. Refer to the [Replicating Data section](#replicating-data) for more details.

Note that if multiple matchers are specified, a resource must satisfy each top-level matcher (`kinds`, `namespaces`, etc.) to be in scope. Each top-level matcher has its own semantics for what qualifies as a match. An empty matcher is deemed to be inclusive (matches everything).

### Replicating Data

Some constraints are impossible to write without access to more state than just the object under test. For example, it is impossible to know if an ingress's hostname is unique among all ingresses unless a rule has access to all other ingresses. To make such rules possible, we enable syncing of data into OPA.

The audit feature also requires replication. Because we rely on OPA as the source-of-truth for audit queries, an object must first be cached before it can be audited for constraint violations.

Kubernetes data can be replicated into OPA via the sync config resource. Currently resources defined in `syncOnly` will be synced into OPA. Updating `syncOnly` should dynamically update what objects are synced. Below is an example:

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  sync:
    syncOnly:
      - group: ""
        version: "v1"
        kind: "Namespace"
      - group: ""
        version: "v1"
        kind: "Pod"
```

You can install this config with the following command:

```sh
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/demo/basic/sync.yaml
```

Once data is synced into OPA, rules can access the cached data under the `data.inventory` document.

The `data.inventory` document has the following format:

  * For cluster-scoped objects: `data.inventory.cluster[<groupVersion>][<kind>][<name>]`
     * Example referencing the Gatekeeper namespace: `data.inventory.cluster["v1"].Namespace["gatekeeper"]`
  * For namespace-scoped objects: `data.inventory.namespace[<namespace>][groupVersion][<kind>][<name>]`
     * Example referencing the Gatekeeper pod: `data.inventory.namespace["gatekeeper"]["v1"]["Pod"]["gatekeeper-controller-manager-d4c98b788-j7d92"]`

### Audit

The audit functionality enables periodic evaluations of replicated resources against the policies enforced in the cluster to detect pre-existing misconfigurations. Audit results are stored as violations listed in the `status` field of the failed constraint.

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
status:
  auditTimestamp: "2019-05-11T01:46:13Z"
  enforced: true
  violations:
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: default
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: gatekeeper-system
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: kube-public
  - enforcementAction: deny
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: kube-system
```
> NOTE: Audit requires replication of Kubernetes resources into OPA before they can be evaluated against the enforced policies. Refer to the [Replicating data](#replicating-data) section for more information.

- Audit interval: set `--audit-interval=123` (defaults to every `60` seconds)
- Audit violations per constraint: set `--constraint-violations-limit=123` (defaults to `20`)
- Disable: set `--audit-interval=0`

### Log denies / dryrun

Set the `--log-denies` flat to log all denies and dryrun failures.
This is useful when trying to see what is being denied/fails dry-run and keeping a log to debug cluster problems without having to enable syncing or looking through the status of all constraints.

### Dry Run

When rolling out new constraints to running clusters, the dry run functionality can be helpful as it enables constraints to be deployed in the cluster without making actual changes. This allows constraints to be tested in a running cluster without enforcing them. Cluster resources that are impacted by the dry run constraint are surfaced as violations in the `status` field of the constraint. 

To use the dry run feature, add `enforcementAction: dryrun` to the constraint spec to ensure no actual changes are made as a result of the constraint. By default, `enforcementAction` is set to `deny` as the default behavior is to deny admission requests with any violation. 

For example:
```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: ns-must-have-gk
spec:
  enforcementAction: dryrun
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
  parameters:
    labels: ["gatekeeper"]
status:
  auditTimestamp: "2019-08-15T01:46:13Z"
  enforced: true
  violations:
  - enforcementAction: dryrun
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: default
  - enforcementAction: dryrun
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: gatekeeper-system

```
> NOTE: The supported enforcementActions are [`deny`, `dryrun`] for constraints. Update the `--disable-enforcementaction-validation=true` flag if the desire is to disable enforcementAction validation against the list of supported enforcementActions.

### Debugging

> NOTE: Verbose logging with DEBUG level can be turned on with `--log-level=DEBUG`.  By default, the `--log-level` flag is set to minimum log level `INFO`. Acceptable values for minimum log level are [`DEBUG`, `INFO`, `WARNING`, `ERROR`]. In production, this flag should not be set to `DEBUG`.

In debugging decisions and constraints, a few pieces of information can be helpful:

   * Cached data and existing rules at the time of the request
   * A trace of the evaluation
   * The input document being evaluated

Writing out this information for every request would be very expensive, and it would be hard
to find the relevant logs for a given request. Instead, Gatekeeper allows users to specify
resources and requesting users for which information will be logged. They can do so by
configuring the `Config` resource, which lives in the `gatekeeper-system` namespace.

Below is an example of a config resource:

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  # Data to be replicated into OPA
  sync:
    syncOnly:
      - group: ""
        version: "v1"
        kind: "Namespace"
  validation:
    # Requests for which we want to run traces
    traces:
        # The requesting user for which traces will be run
      - user: "user_to_trace@company.com"
        kind:
          # The group, version, kind for which we want to run a trace
          group: ""
          version: "v1"
          kind: "Namespace"
          # If dump is defined and set to `All`, also dump the state of OPA
          dump: "All"
```

Traces will be written to the stdout logs of the Gatekeeper controller.


If there is an error in the Rego in the ConstraintTemplate, there are cases where it is still created via `kubectl apply -f [CONSTRAINT_TEMPLATE_FILENAME].yaml`.

When applying the constraint using `kubectl apply -f constraint.yaml` with a ConstraintTemplate that contains incorrect Rego, and error will occur: `error: unable to recognize "[CONSTRAINT_FILENAME].yaml": no matches for kind "[NAME_OF_CONSTRAINT]" in version "constraints.gatekeeper.sh/v1beta1"`.

To find the error, run `kubectl get -f [CONSTRAINT_FILENAME].yaml -oyaml`. Build errors are shown in the `status` field.

## Kick The Tires

The [demo/basic](https://github.com/open-policy-agent/gatekeeper/tree/master/demo/basic) directory contains the above examples of simple constraints, templates and configs to play with. The [demo/agilebank](https://github.com/open-policy-agent/gatekeeper/tree/master/demo/agilebank) directory contains more complex examples based on a slightly more realistic scenario. Both folders have a handy demo script to step you through the demos.

# FAQ

## Finalizers

### Why does Gatekeeper add sync finalizers?

When Gatekeeper syncs resources it's adding them to OPA's internal cache. This
cache may be used by constraints to render decisions. Because of this stale data
is bad. It can lead to invalid rejections (e.g. when a uniqueness constraint is
violated because an update conflicts with a since-deleted resource), or invalid
acceptance (e.g. if a constraint uses the cache to make sure a Deployment exists
before a Service can be created). Finalizers help avoid stale state by making
sure Gatekeeper has processed the deletion and removed the object from its cache
before the API Server can garbage collect the object.

### How can I remove finalizers? Why are they hanging around?

If Gatekeeper is running, it should automatically clean up the finalizer. If it
isn't this is a misbehavior that should be investigated. Please file a bug with
as much data as you can gather. Including logs, memory usage and utilization, CPU usage and
utilization and any other information that may be helpful.

If Gatekeeper is not running:

* If it did not have a clean exit, Gatekeeper's garbage collection routine would
  have been unable to run. Reasons for an unclean exit are:
  * The service account was deleted before the Pod exited, blocking the GC
    process (this can happen if you delete the gatekeeer-system namespace
    before deleting the deployment or deleting the manifest all at
    once).
  * The container was sent a hard kill signal
  * The container had a panic

It is safest to remove the Config resource before uninstalling Gatekeeper, as
that causes finalizers to be removed outside of the normal GC process.

Finalizers can be removed manually via `kubectl edit` or `kubectl patch`
