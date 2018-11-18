deny[{
	"id": "exec-pods-kube-system",
	"resolution": "Your're not allowed to exec/cp on Pods in kube-system",
}] {
	input.kind = "SubjectAccessReview"
	input.apiVersion = "authorization.k8s.io/v1beta1"

	not user_system_control_plane(input.spec.user)
	input.spec.resourceAttributes.namespace = "kube-system"
	input.spec.resourceAttributes.verb = "create"
	input.spec.resourceAttributes.subresource = "exec"
}
