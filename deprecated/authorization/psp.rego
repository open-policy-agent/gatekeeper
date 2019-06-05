package authorization

import data.k8s.matches

deny[{
	"id": "podsecuritypolicies-kube-system",
    "resource": {
        "kind": kind,
        "namespace": namespace,
        "name": name,
    },
	"resolution": {"message": "Your're not allowed to use the privileged PodSecurityPolicies in pods outside of kube-system and istio-system"},
}] {
    matches[[kind, namespace, name, resource]]

	resource.spec.resourceAttributes.group = "extensions"
	resource.spec.resourceAttributes.resource = "podsecuritypolicies"
	resource.spec.resourceAttributes.name = "privileged"
	not re_match("^(kube-system|istio-system)$", resource.spec.resourceAttributes.namespace)
	resource.spec.resourceAttributes.verb = "use"
}
