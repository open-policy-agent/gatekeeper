package logging

// Log keys.
const (
	Process              = "process"
	Details              = "details"
	EventType            = "event_type"
	TemplateName         = "template_name"
	ConstraintNamespace  = "constraint_namespace"
	ConstraintName       = "constraint_name"
	ConstraintGroup      = "constraint_group"
	ConstraintKind       = "constraint_kind"
	ConstraintAPIVersion = "constraint_api_version"
	ConstraintStatus     = "constraint_status"
	ConstraintAction     = "constraint_action"
	AuditID              = "audit_id"
	ConstraintViolations = "constraint_violations"
	ResourceGroup        = "resource_group"
	ResourceKind         = "resource_kind"
	ResourceAPIVersion   = "resource_api_version"
	ResourceNamespace    = "resource_namespace"
	ResourceName         = "resource_name"
	RequestUsername      = "request_username"
	MutationApplied      = "mutation_applied"
	Mutator              = "mutator"
	DebugLevel           = 2 // r.log.Debug(foo) == r.log.V(logging.DebugLevel).Info(foo)
)
