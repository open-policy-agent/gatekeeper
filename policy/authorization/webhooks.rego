package authorization

deny[{
	"id": "webhookconfigurations-dhc-prefix",
	"resolution": "You're not allowed to create/update/delete ValidatingWebhookConfigurations & MutatingWebhookConfigurations starting with 'system'",
}] {
	input.kind = "SubjectAccessReview"
	input.apiVersion = "authorization.k8s.io/v1beta1"

	not user_system_control_plane(input.spec.user)
	input.spec.resourceAttributes.group = "admissionregistration.k8s.io"
	re_match("^(validatingwebhookconfigurations|mutatingwebhookconfigurations)$", input.spec.resourceAttributes.resource)

	is_system(input.spec.resourceAttributes.name)
	match_cud(input.spec.resourceAttributes.verb)
}
