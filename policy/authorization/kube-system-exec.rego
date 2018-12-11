package authorization

import data.k8s.matches

deny[{
	"id": "exec-pods-kube-system",
	"resource": {
		"kind": kind,
		"namespace": namespace,
		"name": name,
	},
	"resolution": {"message": "Your're not allowed to exec/cp on Pods in kube-system"},
}] {
	matches[[kind, namespace, name, resource]]

	not user_system_control_plane(resource.spec.user)
	resource.spec.resourceAttributes.namespace = "kube-system"
	resource.spec.resourceAttributes.verb = "create"
	resource.spec.resourceAttributes.subresource = "exec"
}
