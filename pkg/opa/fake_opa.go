package opa

import (
	"regexp"

	"github.com/Azure/kubernetes-policy-controller/pkg/policies/types"
)

// FakeOPA is a OPA mock used for unit testing
type FakeOPA struct {
	allViolations map[string][]types.Deny
	allPatches    map[string][]types.Patch
}

// SetViolation sets a violation for a query pattern
func (o *FakeOPA) SetViolation(querypattern string, violations ...types.Deny) {
	if o.allViolations == nil {
		o.allViolations = map[string][]types.Deny{}
	}
	for _, v := range violations {
		o.allViolations[querypattern] = append(o.allViolations[querypattern], v)
	}
}

// PostQuery implements the PostQuest OPA client interface and returns
// violations based matching query patterns set using SetViolations api
func (o *FakeOPA) PostQuery(query string) ([]map[string]interface{}, error) {
	result := []map[string]interface{}{}
	if o.allViolations == nil {
		return result, nil
	}
	for p, vs := range o.allViolations {
		if matched, _ := regexp.MatchString(p, query); !matched {
			continue
		}
		for _, v := range vs {
			x := map[string]interface{}{}
			x["v"] = v
			result = append(result, x)
		}
	}

	return result, nil
}

// MakeDenyObject is helped menthod to make deny object
func MakeDenyObject(id, kind, name, namespace, message string) types.Deny {
	return types.Deny{
		ID: id,
		Resource: types.Resource{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		},
		Message: message,
	}
}

// MakePatchObject is helped menthod to make deny object
func MakePatchObject(id, kind, name, namespace, message string) types.Deny {
	return types.Deny{
		ID: id,
		Resource: types.Resource{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		},
		Message: message,
	}
}
