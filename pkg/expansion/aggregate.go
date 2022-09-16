package expansion

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
)

const childMsgPrefix = "[Implied by %s]"

// AggregateResponses aggregates all responses from children into the parent.
// Child result messages will be prefixed with a string to indicate the msg
// is implied by a ExpansionTemplate.
func AggregateResponses(templateName string, parent *types.Responses, child *types.Responses) {
	for target, childRes := range child.ByTarget {
		addPrefixToChildMsgs(templateName, childRes)
		if parentResp, exists := parent.ByTarget[target]; exists {
			parentResp.Results = append(parentResp.Results, childRes.Results...)
		} else {
			parent.ByTarget[target] = childRes
		}
	}
}

func addPrefixToChildMsgs(templateName string, res *types.Response) {
	for _, r := range res.Results {
		r.Msg = fmt.Sprintf("%s %s", fmt.Sprintf(childMsgPrefix, templateName), r.Msg)
	}
}
