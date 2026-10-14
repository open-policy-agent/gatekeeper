package target

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
)

func processFromEnforcementPoint(enforcementPoint string) process.Process {
	switch enforcementPoint {
	case util.AuditEnforcementPoint:
		return process.Audit
	default:
		return process.Webhook
	}
}
