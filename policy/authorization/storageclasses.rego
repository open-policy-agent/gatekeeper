package authorization

import data.k8s.matches

deny[{
	"id": "storageclasses",
    "resource": {
        "kind": "storageclasses",
        "namespace": namespace,
        "name": name,
    },
	"resolution":  {"message": "Your're not allowed to create/update/delete StorageClasses 'cinder'"},
}] {
	matches[[kind, namespace, name, resource]]

	re_match("^(cinder)$", resource.spec.resourceAttributes.name)
	re_match("^(create|patch|update|replace|delete|deletecollections)$", resource.spec.resourceAttributes.verb)
}
