package authorization

import data.k8s.matches

deny[{
	"id": "webhookconfigurations-system",
    "resource": {
        "kind": kind,
        "namespace": namespace,
        "name": name,
    },
	"resolution": {"message": "You're not allowed to create/update/delete ValidatingWebhookConfigurations & MutatingWebhookConfigurations starting with 'system'"},
}] {
	matches[[kind, namespace, name, resource]]

	not user_system_control_plane(resource.spec.user)
	resource.spec.resourceAttributes.group = "admissionregistration.k8s.io"
	re_match("^(validatingwebhookconfigurations|mutatingwebhookconfigurations)$", resource.spec.resourceAttributes.resource)

	is_system(resource.spec.resourceAttributes.name)
	match_cud(resource.spec.resourceAttributes.verb)
}
