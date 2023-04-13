package logging

import (
	"github.com/go-logr/logr"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
)

// Log keys.
const (
	Process               = "process"
	Details               = "details"
	EventType             = "event_type"
	TemplateName          = "template_name"
	ConstraintNamespace   = "constraint_namespace"
	ConstraintName        = "constraint_name"
	ConstraintGroup       = "constraint_group"
	ConstraintKind        = "constraint_kind"
	ConstraintAPIVersion  = "constraint_api_version"
	ConstraintStatus      = "constraint_status"
	ConstraintAction      = "constraint_action"
	ConstraintAnnotations = "constraint_annotations"
	AuditID               = "audit_id"
	ConstraintViolations  = "constraint_violations"
	ResourceGroup         = "resource_group"
	ResourceKind          = "resource_kind"
	ResourceLabels        = "resource_labels"
	ResourceAPIVersion    = "resource_api_version"
	ResourceNamespace     = "resource_namespace"
	ResourceName          = "resource_name"
	RequestUsername       = "request_username"
	MutationApplied       = "mutation_applied"
	Mutator               = "mutator"
	DebugLevel            = 2 // r.log.Debug(foo) == r.log.V(logging.DebugLevel).Info(foo)
)

func LogStatsEntries(client *constraintclient.Client, logger logr.Logger, entries []*instrumentation.StatsEntry, msg string) {
	for _, se := range entries {
		labelledLogger := logger
		for _, label := range se.Labels {
			labelledLogger = labelledLogger.WithValues(label.Name, label.Value)
		}

		for _, stat := range se.Stats {
			labelledLogger = labelledLogger.WithValues(
				"scope", se.Scope,
				"statsFor", se.StatsFor,
				"source_type", stat.Source.Type,
				"source_value", stat.Source.Value,
				"name", stat.Name,
				"value", stat.Value,
			)

			if client != nil {
				desc := client.GetDescriptionForStat(stat.Source, stat.Name)
				labelledLogger = labelledLogger.WithValues("description", desc)
			}

			labelledLogger.Info(msg)
		}
	}
}
