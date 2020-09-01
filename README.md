# Gatekeeper

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

## Admission Webhook Fail-Open Status

Currently Gatekeeper is defaulting to using `failurePolicy​: ​Ignore` for admission request webhook errors. The impact of
this is that when the webhook is down, or otherwise unreachable, constraints will not be
enforced. Audit is expected to pick up any slack in enforcement by highlighting invalid
resources that made it into the cluster.

The reason for fail-open is because the webhook server currently only has one instance, which risks downtime
during actions like upgrades. If we were to fail closed, this downtime would lead to
downtime in the cluster's control plane. We are currently working on addressing issues
that may cause multi-pod deployments of Gatekeeper to not work as expected. Once
we can improve availability by running in multiple pods, we will likely make
that setup the default and change our default webhook behavior to fail-closed (`failurePolicy: Fail`).

If desired, the webhook can be set to fail-closed by modifying the ValidatingWebhookConfiguration,
though this may have uptime impact on your cluster's control plane. In the interim,
it is best to avoid policies that assume 100% enforcement during request
time (e.g. mimicking RBAC-like behavior by validating the user making the request).

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
       * You have Docker version 19.03 or later installed.
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
      make docker-buildx REPOSITORY="$DESTINATION_GATEKEEPER_DOCKER_IMAGE"
      make docker-push-release REPOSITORY="$DESTINATION_GATEKEEPER_DOCKER_IMAGE"
      ```
   * Finally, deploy:
     ```sh
     make deploy REPOSITORY="$DESTINATION_GATEKEEPER_DOCKER_IMAGE"
     ```

#### Deploying via Helm ####

A basic Helm v2 template exists in `charts/gatekeeper`. If you have Helm installed and Tiller initialized on your cluster you can deploy via
```sh
helm repo add gatekeeper https://open-policy-agent.github.io/gatekeeper/charts
helm install gatekeeper/gatekeeper --devel
```

Please note that this chart is not compatible with Helm 3 at this time.

You can alter the variables in `charts/gatekeeper/values.yaml` to customize your deployment. To regenerate the base template, run `make manifests`.

### Uninstallation

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
   * `scope` accepts `*`, `Cluster`, or `Namespaced` which determines if cluster-scoped and/or namesapced-scoped resources are selected. (defaults to `*`)
   * `namespaces` is a list of namespace names. If defined, a constraint will only apply to resources in a listed namespace.
   * `excludedNamespaces` is a list of namespace names. If defined, a constraint will only apply to resources not in a listed namespace.
   * `labelSelector` is a standard Kubernetes label selector.
   * `namespaceSelector` is a standard Kubernetes namespace selector. If defined, make sure to add `Namespaces` to your `configs.config.gatekeeper.sh` object to ensure namespaces are synced into OPA. Refer to the [Replicating Data section](#replicating-data) for more details.

Note that if multiple matchers are specified, a resource must satisfy each top-level matcher (`kinds`, `namespaces`, etc.) to be in scope. Each top-level matcher has its own semantics for what qualifies as a match. An empty matcher is deemed to be inclusive (matches everything). Also understand `namespaces`, `excludedNamespaces`, and `namespaceSelector` will match on cluster scoped resources which are not namespaced. To avoid this adjust the `scope` to `Namespaced`.

### Replicating Data

Some constraints are impossible to write without access to more state than just the object under test. For example, it is impossible to know if an ingress's hostname is unique among all ingresses unless a rule has access to all other ingresses. To make such rules possible, we enable syncing of data into OPA.

The audit feature does not require replication by default. However, when the ``audit-from-cache`` flag is set to true, the OPA cache will be used as the source-of-truth for audit queries; thus, an object must first be cached before it can be audited for constraint violations.

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

- Audit interval: set `--audit-interval=123` (defaults to every `60` seconds)
- Audit violations per constraint: set `--constraint-violations-limit=123` (defaults to `20`)
- Disable: set `--audit-interval=0`

By default, the audit will request each resource from the Kubernetes API during each cycle of the audit. To instead rely on the OPA cache, use the flag `--audit-from-cache=true`. Note that this requires replication of Kubernetes resources into OPA before they can be evaluated against the enforced policies. Refer to the [Replicating data](#replicating-data) section for more information.

#### Audit using kinds specified in the constraints only

By default, Gatekeeper will audit all resources in the cluster. This operation can take some time depending on the number of resources.

If all of your constraints match against specific kinds (e.g. "match only pods"), then you can speed up audit runs by setting `--audit-match-kind-only=true` flag. This will only check resources of the kinds specified in all [constraints](#Constraints) defined in the cluster.

For example, defining this constraint will only audit `Pod` kind:

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sAllowedRepos
metadata:
  name: prod-repo-is-openpolicyagent
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
...
```

If any of the [constraints](#Constraints) do not specify `kinds`, it will be equivalent to not setting ``--audit-match-kind-only` flag (`false` by default), and will fall back to auditing all resources in the cluster.

### Log denies

Set the `--log-denies` flag to log all denies and dryrun failures.
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

### Exempting Namespaces from Gatekeeper

Config resource can be used as follows to exclude namespaces from certain processes for all constraints in the cluster. To exclude namespaces at a constraint level, use `excludedNamespaces` in the [constraint](#constraints) instead.

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  match:
    - excludedNamespaces: ["kube-system", "gatekeeper-system"]
      processes: ["*"]
    - excludedNamespaces: ["audit-excluded-ns"]
      processes: ["audit"]
    - excludedNamespaces: ["audit-webhook-sync-excluded-ns"]
      processes: ["audit", "webhook", "sync"]
...
```

Available processes:
- `audit` process exclusion will exclude resources from specified namespace(s) in audit results.
- `webhook` process exclusion will exclude resources from specified namespace(s) from the admission webhook.
- `sync` process exclusion will exclude resources from specified namespace(s) from being synced into OPA.
- `*` includes all current processes above and includes any future processes.

#### Exempting Namespaces from the Gatekeeper Admission Webhook using `--exempt-namespace` flag

Note that the following only exempts resources from the admission webhook. They will still be audited. Editing individual constraints is
necessary to exclude them from audit.

If it becomes necessary to exempt a namespace from Gatekeeper entirely (e.g. you want `kube-system` to bypass admission checks), here's how to do it:

   1. Make sure the validating admission webhook configuration for Gatekeeper has the following namespace selector:

        ```yaml
          namespaceSelector:
            matchExpressions:
            - key: admission.gatekeeper.sh/ignore
              operator: DoesNotExist
        ```
      the default Gatekeeper manifest should already have added this. The default name for the
      webhook configuration is `gatekeeper-validating-webhook-configuration` and the default
      name for the webhook that needs the namespace selector is `validation.gatekeeper.sh`

   2. Tell Gatekeeper it's okay for the namespace to be ignored by adding a flag to the pod:
      `--exempt-namespace=<NAMESPACE NAME>`. This step is necessary because otherwise the
      permission to modify a namespace would be equivalent to the permission to exempt everything
      in that namespace from policy checks. This way a user must explicitly have permissions
      to configure the Gatekeeper pod before they can add exemptions.

   3. Add the `admission.gatekeeper.sh/ignore` label to the namespace. The value attached
      to the label is ignored, so it can be used to annotate the reason for the exemption.

### Debugging

> NOTE: Verbose logging with DEBUG level can be turned on with `--log-level=DEBUG`.  By default, the `--log-level` flag is set to minimum log level `INFO`. Acceptable values for minimum log level are [`DEBUG`, `INFO`, `WARNING`, `ERROR`]. In production, this flag should not be set to `DEBUG`.


### Enable Delete Operations
To enable Delete operations for the `validation.gatekeeper.sh` admission webhook, add "DELETE" to the list of operations in the `gatekeeper-validating-webhook-configuration` ValidatingWebhookConfiguration as seen in this deployment manifest of gatekeeper: [here](https://github.com/open-policy-agent/gatekeeper/blob/v3.1.0-beta.10/deploy/gatekeeper.yaml#L792-L794)
Note: For admission webhooks registered for DELETE operations, use Kubernetes v1.15.0+

 So you have
 ```YAML
    operations:
    - CREATE
    - UPDATE
    - DELETE
```

You can now check for deletes.

#### Viewing the Request Object

A simple way to view the request object is to use a constraint/template that
denies all requests and outputs the request object as its rejection message.

Example template:

```yaml
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: k8sdenyall
spec:
  crd:
    spec:
      names:
        kind: K8sDenyAll
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdenyall

        violation[{"msg": msg}] {
          msg := sprintf("REVIEW OBJECT: %v", [input.review])
        }
```

Example constraint:

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sDenyAll
metadata:
  name: deny-all-namespaces
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
```

#### Tracing

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

### Customizing Admission Behavior

Gatekeeper is a [Kubernetes admission webhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#webhook-configuration)
whose default configuration can be found in the `gatekeeper.yaml` manifest file. By default, it is
a `ValidatingWebhookConfiguration` resource named `gatekeeper-validating-webhook-configuration`.

Currently the configuration specifies two webhooks: one for checking a request against
the installed constraints and a second webhook for checking labels on namespace requests
that would result in bypassing constraints for the namespace. The namespace-label webhook
is necessary to prevent a privilege escalation where the permission to add a label to a
namespace is equivalent to the ability to bypass all constraints for that namespace.
You can read more about the ability to exempt namespaces by label [above](#exempting-namespaces-from-the-gatekeeper-admission-webhook).

Because Kubernetes adds features with each version, if you want to know how the webhook can be configured it
is best to look at the official documentation linked at the top of this section. However, two particularly important
configuration options deserve special mention: [timeouts](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#timeouts) and
[failure policy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy).

Timeouts allow you to configure how long the API server will wait for a response from the admission webhook before it
considers the request to have failed. Note that setting the timeout longer than the overall request timeout
means that the main request will time out before the webhook's failure policy is invoked.

Failure policy controls what happens when a webhook fails for whatever reason. Common
failure scenarios include timeouts, a 5xx error from the server or the webhook being unavailable.
You have the option to ignore errors, allowing the request through, or failing, rejecting the request.
This results in a direct tradeoff between availability and enforcement.

Currently Gatekeeper is defaulting to using `Ignore` for the constraint requests. This is because
the webhook server currently only has one instance, which risks downtime during actions like upgrades.
As the theoretical availability improves we will likely change the default to `Fail`.

The namespace label webhook defaults to `Fail`, this is to help ensure that policies preventing
labels that bypass the webhook from being applied are enforced. Because this webhook only gets
called for namespace modification requests, the impact of downtime is mitigated, making the
theoretical maximum availability less of an issue.

Because the manifest is available for customization, the webhook configuration can
be tuned to meet your specific needs if they differ from the defaults.

### Emergency Recovery

If a situation arises where Gatekeeper is preventing the cluster from operating correctly,
the webhook can be disabled. This will remove all Gatekeeper admission checks. Assuming
the default webhook name has been used this can be achieved by running:

`kubectl delete validatingwebhookconfigurations.admissionregistration.k8s.io gatekeeper-validating-webhook-configuration`

Redeploying the webhook configuration will re-enable Gatekeeper.

### Running on private GKE Cluster nodes

By default, firewall rules restrict the cluster master communication to nodes only on ports 443 (HTTPS) and 10250 (kubelet). Although Gatekeeper exposes its service on port 443, GKE by default enables `--enable-aggregator-routing` option, which makes the master to bypass the service and communicate straight to the POD on port 8443.

Two ways of working around this:

- create a new firewall rule from master to private nodes to open port `8443` (or any other custom port)
  - https://cloud.google.com/kubernetes-engine/docs/how-to/private-clusters#add_firewall_rules
- make the pod to run on privileged port 443 (need to run pod as root)
  - update Gatekeeper deployment manifest spec:
    - remove `securityContext` settings that force the pods not to run as root
    - update port from `8443` to `443`
    ```yaml
    containers:
    - args:
      - --port=443
      ports:
      - containerPort: 443
        name: webhook-server
        protocol: TCP
    ```

  - update Gatekeeper service manifest spec:
    - update `targetPort` from `8443` to `443`
    ```yaml
    ports:
    - port: 443
      targetPort: 443
    ```

## Kick The Tires

The [demo/basic](https://github.com/open-policy-agent/gatekeeper/tree/master/demo/basic) directory contains the above examples of simple constraints, templates and configs to play with. The [demo/agilebank](https://github.com/open-policy-agent/gatekeeper/tree/master/demo/agilebank) directory contains more complex examples based on a slightly more realistic scenario. Both folders have a handy demo script to step you through the demos.

# Security

Please report vulnerabilities by email to [open-policy-agent-security](mailto:open-policy-agent-security@googlegroups.com).
We will send a confirmation message to acknowledge that we have received the
report and then we will send additional messages to follow up once the issue
has been investigated.

For details on the security release process please refer to the [open-policy-agent/opa/SECURITY.md](https://github.com/open-policy-agent/opa/blob/master/SECURITY.md) file.
