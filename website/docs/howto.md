---
id: howto
title: How to use Gatekeeper
---

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

### Running on OpenShift 4.x

When running on OpenShift, the `nouid` scc must be used to keep a restricted profile but being able to set the UserID.

In order to use it, the following section must be added to the gatekeeper-manager-role Role:

```yaml
- apiGroups:
  - security.openshift.io
  resourceNames:
    - anyuid
  resources:
    - securitycontextconstraints
  verbs:
    - use
```

With this restricted profile, it won't be possible to set the `container.seccomp.security.alpha.kubernetes.io/manager: runtime/default` annotation. On the other hand, given the limited amount of privileges provided by the anyuid scc, the annotation can be removed.