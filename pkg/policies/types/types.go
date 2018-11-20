package types

import (
	"fmt"
)

// AuditResponseV1 models audit response from the server
type AuditResponseV1 struct {
	Message    string `json:"message,omitempty"`
	Violations []Deny `json:"violations,omitempty"`
}

// Resource models metadata for kubernetes resource
type Resource struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// Resolution models a resolution for a resource violation on the enabled policy rules
type Resolution struct {
	Message string           `json:"message,omitempty"`
	Patches []PatchOperation `json:"patches,omitempty"`
}

// Deny models a resource violation on the enabled policy rules
type Deny struct {
	ID         string     `json:"id,omitempty"`
	Resource   Resource   `json:"resource,omitempty"`
	Resolution Resolution `json:"resolution,omitempty"`
}

// PatchOperation models a patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
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
	
		matches[[kind, namespace, name, resource]] {
			resource := kubernetes[kind][namespace][name].object
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
	return `data.admission.deny[{
		"id": id,
		"resource": {"kind": kind, "namespace": namespace, "name": name},
		"resolution": resolution,}]`
}

// MakeSingleNamespaceResourceQuery makes a single resource query
func MakeSingleNamespaceResourceQuery(resource, namespace, name string) string {
	return fmt.Sprintf(`data.admission.deny[{
			"id": id,
			"resource": {"kind": "%s", "namespace": "%s", "name": "%s"},
			"resolution": resolution,}]`,
		resource, namespace, name)
}

// MakeSingleClusterResourceQuery makes a single resource query
func MakeSingleClusterResourceQuery(resource, name string) string {
	return fmt.Sprintf(`data.admission.deny[{
			"id": id,
			"resource": {"kind": "%s", "name": "%s"},
			"resolution": resolution,}]`,
		resource, name)
}

// MakeSingleNamespaceResourceQuery makes a single resource query
// For now I would keep the separation of the OPA packages here, because
// the values which are given later via the value just don't have the same
// format. But at least the rules have a similar structure now.
func MakeSingleNamespaceAuthorizationResourceQuery(resource, namespace, name string) string {
	return fmt.Sprintf(`data.authorization.deny[{
			"id": id,
			"resource": {"kind": "%s", "namespace": "%s", "name": "%s"},
			"resolution": resolution,}]`,
		resource, namespace, name)
}
