package expansion

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
)

const childMsgPrefix = "[mock resource created from expanding %s]"

// AggregateResponses aggregates all responses from children into the parent.
// Child result messages will be prefixed with a string to indicate the msg
// is from a mock child resource.
func AggregateResponses(parentName string, parent *types.Responses, children []*types.Responses) {
	for _, child := range children {
		for target, childRes := range child.ByTarget {
			addPrefixToChildMsgs(parentName, childRes)
			if parentResp, exists := parent.ByTarget[target]; exists {
				parentResp.Results = append(parentResp.Results, childRes.Results...)
			} else {
				parent.ByTarget[target] = childRes
			}
		}
	}
}

func addPrefixToChildMsgs(parentName string, res *types.Response) {
	for _, r := range res.Results {
		r.Msg = fmt.Sprintf("%s %s", fmt.Sprintf(childMsgPrefix, parentName), r.Msg)
	}
}
