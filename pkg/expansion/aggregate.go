package expansion

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
)

const (
	childMsgPrefix = "[Implied by %s]"

	ChildStatLabel = "Implied by"
)

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

// AggregateStats aggregates all stats from the child Responses.StatsEntry
// into the parent Responses.StatsEntry. Child Stats will have a label to
// indicate that they come from an ExpansionTemplate usage.
func AggregateStats(templateName string, parent *types.Responses, child *types.Responses) {
	childStatsEntries := child.StatsEntries

	for _, se := range childStatsEntries {
		se.Labels = append(se.Labels, []*instrumentation.Label{{
			Name:  ChildStatLabel,
			Value: templateName,
		}}...)
	}

	parent.StatsEntries = append(parent.StatsEntries, child.StatsEntries...)
}

func OverrideEnforcementAction(action string, resps *types.Responses) {
	// If the enforcement action is empty, do not override
	if action == "" {
		return
	}

	for _, resp := range resps.ByTarget {
		for _, res := range resp.Results {
			res.EnforcementAction = action
		}
	}
}

func addPrefixToChildMsgs(templateName string, res *types.Response) {
	for _, r := range res.Results {
		r.Msg = fmt.Sprintf("%s %s", fmt.Sprintf(childMsgPrefix, templateName), r.Msg)
	}
}
