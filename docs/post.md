# Kubernetes clusters governance made simple

## Overview

Every organization has some rules. Some of these are essential to meet governance and legal requirements and other are based on learning from past expereince and not repeating the same mistakes. These desicions cannot tolerate human response time as they need near real time action. Services that are policy enabled make the organization agile and are essential for long term success as they are more adaptable as violations and conflicts can be discovered consistently as they are not prone to human error. 

Kubernetes compliance is enforced at the “runtime” via tools such as network policy and pod security policy. [kubernetes-policy-controller](https://github.com/Azure/kubernetes-policy-controller) extends the compliance enforcement at “create” event not at “run“ event. For example, a kubernetes service could answer questions like :

* Can we White list/ black list registries.
* Not allow conflicting hosts for ingresses.
* Label objects based on user from a department.
* What are the policies that my cluster is violating.

We are happy to announce `kubernetes-policy-controller` which allows enforcing custom semantic rules on on objects during create, update and delete operations without recompiling or reconfiguring the Kubernetes API server. The controller is backed by the Open Policy Agent([OPA](https://github.com/open-policy-agent/opa)) which is a light weight, general purpose policy engine for Cloud Native environments.

## How policy works with `kubernetes-policy-controller`

Policy-enablement empowers users to read, write, and manage these rules without needing specialized development. Policy rules can be selected from a library of pre created rules or can be added to the library using Rego using OPA’s native query language Rego following a standard template to define validation and mutation policies. Below if one example of a validation policy:

```python
    deny[{
        "id": "ingress-conflict",
        "resource": {"kind": "ingresses", "namespace": namespace, "name": name},
        "message": "ingress host conflicts with an existing ingress",
    }] {
        # gets the ingress matching the ingress that needs to be validated
        matches[["ingresses", namespace, name, matched_ingress]]
        # gets any other ingress which is already a part of the cluster
        matches[["ingresses", other_ns, other_name, other_ingress]]
        # filters to ingresses in other namespaces
        namespace != other_ns
        other_ingress.spec.rules[_].host == matched_ingress.spec.rules[_].host
    }
```

## Architecture of `kubernetes-policy-controller`

Kubernetes allows decoupling complex logic such as policy decision from the inner working of API Server by means of "admission controllers”. Admission control is a custom logic executed by a webhook. `Kubernetes policy controller` is a mutating and a validating webhook which gets called for matching Kubernetes API server requests by the admission controller to enforce semantic validation of objects during create, update, and delete operations. It uses Open Policy Agent ([OPA](https://github.com/open-policy-agent/opa)) is a policy engine for Cloud Native environments hosted by CNCF as a sandbox level project.

The are following components that for the `kubernetes-policy-controller`

* Admission controller Webhook: This is the service that receives CRUD requests from the API server

* OPA service and Kubernetes management pod: The OPA service is a generic policy engine which evaluates configured policy rules on new objects and eventual consistent state of current objects synced using the Kubernetes management pod. 

## What's next

We are on a journey to make policy enforcement on Kubernetes cluster simple and reliable. Try out the [kubernetes-policy-controller](https://github.com/Azure/kubernetes-policy-controller), contribute in writing policies and share your scenarios and ideas ! To be sure, there is a lot more to do here and we look forward to working together to get it done!

We’ll see you on GitHub!