package authorization

deny[{
	"id": "crds",
	"resolution": "Your're not allowed to create/update/delete CRDs of the group 'crd.projectcalico.org'",
}] {
	input.kind = "SubjectAccessReview"
	input.apiVersion = "authorization.k8s.io/v1beta1"

	input.spec.resourceAttributes.resource = "customresourcedefinitions"
	re_match("^.*(crd.projectcalico.org)$", input.spec.resourceAttributes.name)
	re_match("^(create|patch|update|replace|delete|deletecollections)$", input.spec.resourceAttributes.verb)
}

deny[{
	"id": "crds-resources",
	"resolution": "Your're not allowed to create/update/delete resources of the group 'crd.projectcalico.org'",
}] {
	input.kind = "SubjectAccessReview"
	input.apiVersion = "authorization.k8s.io/v1beta1"

	not re_match("^(calico.*|system:kube-controller-manager|system:kube-scheduler)$", input.spec.user)
	re_match("^(crd.projectcalico.org)$", input.spec.resourceAttributes.group)
	re_match("^(create|patch|update|replace|delete|deletecollections)$", input.spec.resourceAttributes.verb)
}
