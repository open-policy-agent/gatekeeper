package authorization

import data.k8s.matches

deny[{
	"id": "crds",
	"resource": {
		"kind": kind,
		"namespace": namespace,
		"name": name,
	},
	"resolution": {"message": "Your're not allowed to create/update/delete CRDs of the group 'crd.projectcalico.org'"},
}] {
	matches[[kind, namespace, name, resource]]

	resource.spec.resourceAttributes.resource = "customresourcedefinitions"
	re_match("^.*(crd.projectcalico.org)$", resource.spec.resourceAttributes.name)
	re_match("^(create|patch|update|replace|delete|deletecollections)$", resource.spec.resourceAttributes.verb)
}

deny[{
	"id": "crds-resources",
	"resource": {
		"kind": kind,
		"namespace": namespace,
		"name": name,
	},
	"resolution": {"message": "Your're not allowed to create/update/delete resources of the group 'crd.projectcalico.org'"},
}] {
	matches[[kind, namespace, name, resource]]

	not re_match("^(calico.*|system:kube-controller-manager|system:kube-scheduler)$", resource.spec.user)
	re_match("^(crd.projectcalico.org)$", resource.spec.resourceAttributes.group)
	re_match("^(create|patch|update|replace|delete|deletecollections)$", resource.spec.resourceAttributes.verb)
}
