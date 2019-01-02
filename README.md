# kubernetes-policy-controller

Every organization has some rules. Some of these are essential to meet governance, and legal requirements and other are based on learning from past experience and not repeating the same mistakes. These decisions cannot tolerate human response time as they need near a real-time action. Services that are policy enabled to make the organization agile and are essential for long-term success as they are more adaptable as violations and conflicts can be discovered consistently as they are not prone to human error. 

Kubernetes allows decoupling complex logic such as policy decisions from the inner working of the API Server by means of [admission controller webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/). This webhooks are executed whenever a resource is created, updated or deleted and can be used to implement complex custom logic. `kubernetes-policy-controller` is a mutating and a validating webhook that gets called for matching Kubernetes API server requests by the admission controller. Kubernetes also has another extension mechanism for general authorization decisions (not necessarily related to resources) which is called [authorization modules](https://kubernetes.io/docs/reference/access-authn-authz/authorization/). Usually, just the RBAC authorization module is used, but with `kubernetes-policy-controller` it's possible to implement a blacklist in front of RBAC. The `kubernetes-policy-controller` uses Open Policy Agent ([OPA](https://github.com/open-policy-agent/opa)), a policy engine for Cloud Native environments hosted by CNCF as a sandbox-level project.

Kubernetes compliance is enforced at the “runtime” via tools such as network policy and pod security policy. [kubernetes-policy-controller](https://github.com/Azure/kubernetes-policy-controller) extends the compliance enforcement at “create” event not at “run“ event. For example, a kubernetes service could answer questions like :

* Can we whitelist / blacklist registries.
* Not allow conflicting hosts for ingresses.
* Label objects based on a user from a department.

In addition to the `admission` scenario  it helps answer the `audit` question such as:

* What are the policies that my cluster is violating.

In the `authorization` scenario it's possible to block things like `kubectl get`, `kubectl port-forward` or even non-resource requests (all authorized request can be blocked).  

## Status

This is a new project and is in alpha state.

## Slack Channel

To participate and contribute in defining and creating kubernetes policies.
Channel Name: `kubernetes-policy`
[slack channel](https://openpolicyagent.slack.com/messages/CDTN970AX)
[Sign up](https://slack.openpolicyagent.org/)

## Using kubernetes-policy-controller

## 1. Deployment

Access to a Kubernetes cluster with "cluster-admin" permission is the only prerequisite.

Deploy `kubernetes-policy-controller`:

```bash
./deploy/deploy-all.sh
```

Deploy sample policies:

```bash
./deploy/deploy-admission-policy.sh
```

## 2. Scenarios

There are two scenarios of the policy engine namely Validation and Mutation

* Validation: "all resources R in namespace N are taged with annotation A"
* Mutation: "before a resource R in namespace N is created tag it with tag T"  

### 2.1 `validation` scenario

Load the policy as a ConfigMap:

```bash
kubectl create configmap example --from-file ./policy/admission/ingress-host-fqdn.rego
```

```bash
kubectl create ns qa
```

The following call should fail with policy:

```bash
kubectl -n qa apply -f ./demo/ingress-bad.yaml
```

### 2.2 `mutation` scenario

This policy will mutate resources that define an annotation with the key `"test-mutation"`. The resouces will be updated to include the annotation `"foo": "bar"`.

Load the policy as a ConfigMap:

```bash
kubectl create configmap example --from-file ./policy/admission/annotate.rego
```

First create a Deployment:

```bash
kubectl run nginx --image nginx
```

Check that the Deployment was not mutated:

```bash
kubectl get deployment nginx -o json | jq '.metadata'
```

Annotate the Deployment to indicate that it should be mutated:

```bash
kubectl annotate deployment nginx test-mutation=true
```

Check that the Deployment was mutated:

```bash
kubectl get deployment nginx -o json | jq '.metadata'
```

### 2.3 `authorization` scenario

`kubernetes-policy-controller` must be deployed in combination with OPA. In this scenario, `kubenetes-policy-controller` cannot be deployed via the usual mechanisms because the APIServer relies on it for every request. Afaik, the only viable scenario is to deploy it via static pod manifest on all master nodes. The following steps are necessary to configure `kubernetes-policy-controller` as authorization module webhook.

1. Add the authorization module to the APIServer via flag, e.g.: `--authorization-mode=Node,Webhook,RBAC`
1. Configure a webhook config file which is used by the APIServer to call the webhook, e.g.: `--authorization-webhook-config-file=/etc/kubernetes/kubernetes-policy-controller.kubeconfig`. See example file content [here](./deploy/kubernetes-policy-controller.kubeconfig)
1. Deploy the policy-controller via static pod manifest. Place e.g. the following file in `/etc/kubernetes/manifests/`. See example file content [here](./deploy/kubernetes-policy-controller.yaml). In this case no `kube-mgmt` container is deployed, because this would lead to an circular dependency. In this case the policies are stored in the folder `/etc/kubernetes/policy` on the master node. Alternatively, they could be deployed via shared volume and an `initContainer`.
1. Deploy some of the policies stored under [policy/authorization](./policy/authorization). There are examples for:
  1. Blocking create/update/delete on Calico CRDs
  1. Namespace-based blocking of the usage of `privileged` PodSecurityPolicy
  1. Blocking access to StorageClass `cinder`
  1. Blocking create/update/delete on ValidatingWebhookConfigurations & MutatingWebhookConfigurations (which isn't possible via ValidatingWebhooks & MutatingWebhooks)
  1. Blocking `exec` and `cp` on Pods in kube-system

*Note* This authorization modules are never called for users with group `system:masters` 

## create-policy

### policy language

The `kubernetes-policy-controller` uses OPA as the policy engine. OPA provides a high-level declarative language for authoring policies and simple APIs to answer policy queries.
Policy rules are created as a rego files. 

### package admission

`kubernetes-policy-controller` defines a special package name `admission` which is used to logically execute all the `admission` rules.
So any `admission` rule defined should be part of this package.

```go
package admission
```

### deny rule

Each violation of a policy is a `deny` rule. So all we need to capture is all `deny` matches in order to validate.  
In the `admission` package any validation rule should be defined as special name called `deny`. In order to understand the basic idea lets consider a case where we want to create a rule which will block all API server requests i.e fail validation of all requests. The following models an always `deny` event

```go
package admission

deny[{
    "type": "always",
    "resource": {"kind": kind, "namespace": namespace, "name": name},
    "resolution": {"message": "test always violate"},
}] {
    true
}
```

### matches[[kind, namespace, name, matched_resource_output]]  

When defining a deny rule, you must find Kubernetes resources that match specific criteria, such as Ingress resources in a particular namespace. `kubernetes-policy-controller` provides the matches functionality by importing `data.kubernetes.matches`.

```go
import data.kubernetes.matches
```

Here are some examples of how matching can be used:

* Find matching Ingress resources  

```go
import data.kubernetes.matches

matches[["ingress", namespace, name, matched_ingress]]
```

* Find matching "ingress" resources in "production" namespace

```go
import data.kubernetes.matches

matches[["ingress", "production", name, matched_ingress]]
```

* Find matching "ingress" resources in "production" namespace with name "my-ingress"

```go
import data.kubernetes.matches

matches[["ingress", "production", "my-ingress", matched_ingress]]
```

#### Example Policy: Unique Ingress Hostnames

Here is an example of a policy which validates that Ingress hostnames must be unique across Namespaces. This policy shows how you can express a pair-wise search. In this case, there is a violation if any two ingresses in different namespaces have the same hostname. Note, you can query OPA to determine whether a single Ingress violates the policy (in which case the cost is linear with the # of Ingresses) or you can query for the set of all Ingresses that violate the policy (in which case the cost is (# of Ingresses)^2.).
Author : [Torrin Sandall](https://github.com/tsandall)

```go
package admission

import data.kubernetes.matches

deny[{
    "id": "ingress-conflict",
    "resource": {"kind": "ingresses", "namespace": namespace, "name": name},
    "resolution": {"message": "ingress host conflicts with an existing ingress"},
}] {
    matches[["ingresses", namespace, name, matched_ingress]]
    matches[["ingresses", other_ns, other_name, other_ingress]]
    namespace != other_ns
    other_ingress.spec.rules[_].host == matched_ingress.spec.rules[_].host
}

```

#### Example Policy: Container image name from Registry

Below is an example of Container image name check if it matches of the whitelisted patterns e.g. should be from a organization registry.

```go
package admission

import data.k8s.matches

###############################################################################
#
# Policy : Container image name check if it matches of the whitelisted patterns
# e.g. should be from a organization registry. 
#
###############################################################################
deny[{
    "id": "container-image-whitelist",          # identifies type of violation
    "resource": {
        "kind": "pods",                 # identifies kind of resource
        "namespace": namespace,         # identifies namespace of resource
        "name": name                    # identifies name of resource
    },
    "resolution": {"message": msg},     # provides human-readable message to display
}] {
    matches[["pods", namespace, name, matched_pod]]
    container = matched_pod.spec.containers[_]
    not re_match("^registry.acmecorp.com/.+$", container.image)
    msg := sprintf("invalid container registry image %q", [container.image])
}
```

### patches resolution

Patches field allows mutation of objects.

#### Example patch

```js
package admission

import data.k8s.matches

##############################################################################
#
# Policy : Construct JSON Patch for annotating boject with foo=bar if it is
# annotated with "test-mutation"
#
##############################################################################

deny[{
    "id": "conditional-annotation",
    "resource": {"kind": kind, "namespace": namespace, "name": name},
    "resolution": {"patches":  p, "message" : "conditional annotation"},
}] {
    matches[[kind, namespace, name, matched_object]]
    matched_object.metadata.annotations["test-mutation"]
    p = [{"op": "add", "path": "/metadata/annotations/foo", "value": "bar"}]
}
```

### package authorization

`kubernetes-policy-controller` defines a special package name `authorization` which is used to logically execute all the `authorization` rules.
So any `authorization` rule defined should be part of this package.

```go
package authorization
```

#### Example

Similar to the validation example above, authorization rules deny requests when they are matched. In contrast to `admission` rules, for `authorization` 
rules the whole `SubjectAccesReview` request is send to OPA. So now we're able to deny requests on all available attributes of a `SubjectAccessReview`.

```go
package authorization

##############################################################################
#
# Policy : Denys all create/update/delete requests to resources in the group 
# crd.projectcalico.org, except for users calico, system:kube-controller-manager
# and system:kube-scheduler
#
##############################################################################

deny[{
	"id": "crds-resources",
	"resource": {"kind": kind, "namespace": namespace, "name": name},
	"resolution": {"message": "Your're not allowed to create/update/delete resources of the group 'crd.projectcalico.org'"},
}] {
	matches[[kind, namespace, name, resource]]

	not re_match("^(calico|system:kube-controller-manager|system:kube-scheduler)$", input.spec.user)
	re_match("^(crd.projectcalico.org)$", input.spec.resourceAttributes.group)
	re_match("^(create|patch|update|replace|delete|deletecollections)$", input.spec.resourceAttributes.verb)
}
````

### Video

[Demo video of Kubernetes Policy Controller](https://youtu.be/1WObJiTZDHc)

## Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit https://cla.microsoft.com.

When you submit a pull request, a CLA-bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., label, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.
