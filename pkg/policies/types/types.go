package types

import (
	"fmt"
)

// Resource models metadata for kubernetes resource
type Resource struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// Deny models a resource violation on the enabled policy rules
type Deny struct {
	ID       string   `json:"id,omitempty"`
	Resource Resource `json:"resource,omitempty"`
	Message  string   `json:"message,omitempty"`
}

// PatchOperation models a patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// Patch models a resource patch policy
type Patch struct {
	ID       string           `json:"id,omitempty"`
	Resource Resource         `json:"resource,omitempty"`
	Patches  []PatchOperation `json:"patch,omitempty"`
}

var (
	// KubernetesPolicy - Matches provides an abstraction to find resources that match the (kind,
	// namespace, name) triplet.
	KubernetesPolicy = []byte(`
		package k8s
		import data.kubernetes
		
		matches[[kind, namespace, name, resource]] {
			resource := kubernetes[kind][namespace][name]
		}
	`)
	// PolicyMatchPolicy - policymatches provides an abstraction to find policies that match the (name).
	PolicyMatchPolicy = []byte(`
		package k8s
		import data.kubernetes.policies
		
		# Matches provides an abstraction to find policies that match the (name). 
		policymatches[[name, policy]] {
			policy := policies[name]
		}
	`)
)

// MakeAuditQuery query for all deny (policy violations)
func MakeAuditQuery() string {
	return `data.admission.deny[v]`
}

// MakeSingleNamespaceResourceQuery makes a single resource query
func MakeSingleNamespaceResourceQuery(operation, resource, namespace, name string) string {
	switch operation {
	case "deny":
		return fmt.Sprintf(`data.admission.deny[{
			"id": id,
			"resource": {"kind": "%s", "namespace": "%s", "name": "%s"},
			"message": message,}]`,
			resource, namespace, name)
	case "patch":
		return fmt.Sprintf(`patches = [p | data.admission.patch[{
			"id": id,
			"resource": {"kind": "%s", "namespace": "%s", "name": "%s"},
			"patch": ps,}]; p := ps[_]]`,
			resource, namespace, name)
	default:
		panic(fmt.Errorf("unsupported operation(%s)", operation))
	}
}

// MakeSingleNamespaceResourceDenyQuery makes a single resource query
func MakeSingleNamespaceResourceDenyQuery(class, resource, namespace, name string) string {
	return MakeSingleNamespaceResourceQuery("deny", resource, namespace, name)
}

// MakeSingleNamespaceResourcePatchQuery makes a single resource query
func MakeSingleNamespaceResourcePatchQuery(class, resource, namespace, name string) string {
	return MakeSingleNamespaceResourceQuery("patch", resource, namespace, name)
}

// MakeSingleClusterResourceQuery makes a single resource query
func MakeSingleClusterResourceQuery(operation, resource, name string) string {
	switch operation {
	case "deny":
		return fmt.Sprintf(`data.admission.deny[{
			"id": id,
			"resource": {"kind": "%s", "name": "%s"},
			"message": message,}]`,
			resource, name)
	case "patch":
		return fmt.Sprintf(`patches = [p | data.admission.patch[{
			"id": id,
			"resource": {"kind": "%s", "name": "%s"},
			"patch": ps,}]; p := ps[_]]`,
			resource, name)
	default:
		panic(fmt.Errorf("unsupported operation(%s)", operation))
	}
}

// MakeSingleClusterResourceDenyQuery makes a single resource query
func MakeSingleClusterResourceDenyQuery(resource, name string) string {
	return MakeSingleClusterResourceQuery("deny", resource, name)
}

// MakeSingleClusterResourcePatchQuery makes a single resource query
func MakeSingleClusterResourcePatchQuery(resource, name string) string {
	return MakeSingleClusterResourceQuery("patch", resource, name)
}

// MakeDenyObject is helped menthod to make deny object
func MakeDenyObject(id, kind, name, namespace, message string) Deny {
	return Deny{
		ID: id,
		Resource: Resource{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		},
		Message: message,
	}
}
