package logging

import (
	"github.com/go-logr/logr"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	gkinstr "github.com/open-policy-agent/gatekeeper/v3/pkg/instrumentation"
)

// Log keys.
const (
	Process                      = "process"
	Details                      = "details"
	EventType                    = "event_type"
	TemplateName                 = "template_name"
	ConstraintNamespace          = "constraint_namespace"
	ConstraintName               = "constraint_name"
	ConstraintGroup              = "constraint_group"
	ConstraintKind               = "constraint_kind"
	ConstraintAPIVersion         = "constraint_api_version"
	ConstraintStatus             = "constraint_status"
	ConstraintAction             = "constraint_action"
	ConstraintEnforcementActions = "constraint_enforcement_actions"
	ConstraintAnnotations        = "constraint_annotations"
	AuditID                      = "audit_id"
	ConstraintViolations         = "constraint_violations"
	ResourceGroup                = "resource_group"
	ResourceKind                 = "resource_kind"
	ResourceLabels               = "resource_labels"
	ResourceAPIVersion           = "resource_api_version"
	ResourceNamespace            = "resource_namespace"
	ResourceName                 = "resource_name"
	ResourceSourceType           = "resource_source_type"
	RequestUsername              = "request_username"
	MutationApplied              = "mutation_applied"
	Mutator                      = "mutator"
	DebugLevel                   = 1 // r.log.Debug(foo) == r.log.V(logging.DebugLevel).Info(foo)
	ExecutionStats               = "execution_stats"
)

func LogStatsEntries(client *constraintclient.Client, logger logr.Logger, entries []*instrumentation.StatsEntry, msg string) {
	if len(entries) == 0 {
		return
	}

	logger.WithValues(ExecutionStats, gkinstr.ToStatsEntriesWithDesc(client, entries)).Info(msg)
}
