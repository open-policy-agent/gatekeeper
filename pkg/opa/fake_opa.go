package opa

import (
	"encoding/json"
	"regexp"

	"github.com/open-policy-agent/kubernetes-policy-controller/pkg/policies/types"
	opatypes "github.com/open-policy-agent/opa/server/types"
	"github.com/rite2nikhil/opa/util"
)

// FakeOPA is a OPA mock used for unit testing
type FakeOPA struct {
	allViolations map[string][]types.Deny
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
	var result opatypes.QueryResponseV1
	if o.allViolations == nil {
		return result.Result, nil
	}
	type oparesponse struct {
		Result []types.Deny `json:"result"`
	}
	oparesp := oparesponse{Result: []types.Deny{}}
	for p, vs := range o.allViolations {
		if matched, _ := regexp.MatchString(p, query); !matched {
			continue
		}
		for _, v := range vs {
			oparesp.Result = append(oparesp.Result, v)
		}
	}
	bs, err := json.Marshal(oparesp)
	if err != nil {
		return nil, err
	}
	err = util.UnmarshalJSON(bs, &result)
	if err != nil {
		return nil, err
	}

	return result.Result, nil
}

// MakeDenyObject is helped menthod to make deny object
func MakeDenyObject(id, kind, name, namespace, message string, patches []types.PatchOperation) types.Deny {
	return types.Deny{
		ID: id,
		Resource: types.Resource{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		},
		Resolution: types.Resolution{
			Message: message,
			Patches: patches,
		},
	}
}
