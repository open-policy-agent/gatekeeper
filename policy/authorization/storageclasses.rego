package authorization

deny[{
	"id": "storageclasses",
	"resolution": "Your're not allowed to create/update/delete StorageClasses 'cinder'",
}] {
	input.kind = "SubjectAccessReview"
	input.apiVersion = "authorization.k8s.io/v1beta1"

	input.spec.resourceAttributes.resource = "storageclasses"

	re_match("^(cinder)$", input.spec.resourceAttributes.name)
	re_match("^(create|patch|update|replace|delete|deletecollections)$", input.spec.resourceAttributes.verb)
}
