package authorization

deny[{
	"id": "podsecuritypolicies-kube-system",
	"resolution": "Your're not allowed to use the privileged PodSecurityPolicies in pods outside of kube-system and istio-system",
}] {
	input.kind = "SubjectAccessReview"
	input.apiVersion = "authorization.k8s.io/v1beta1"

	input.spec.resourceAttributes.group = "extensions"
	input.spec.resourceAttributes.resource = "podsecuritypolicies"
	input.spec.resourceAttributes.name = "privileged"
	not re_match("^(kube-system|istio-system)$", input.spec.resourceAttributes.namespace)
	input.spec.resourceAttributes.verb = "use"
}
