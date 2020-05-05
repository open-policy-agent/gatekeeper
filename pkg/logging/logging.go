package logging

const (
	Process              = "process"
	EventType            = "event_type"
	TemplateName         = "template_name"
	ConstraintNamespace  = "constraint_namespace"
	ConstraintName       = "constraint_name"
	ConstraintKind       = "constraint_kind"
	ConstraintAPIVersion = "constraint_api_version"
	ConstraintStatus     = "constraint_status"
	ConstraintAction     = "constraint_action"
	AuditID              = "audit_id"
	ConstraintViolations = "constraint_violations"
	ResourceKind         = "resource_kind"
	ResourceAPIVersion   = "resource_api_version"
	ResourceNamespace    = "resource_namespace"
	ResourceName         = "resource_name"
	DebugLevel           = 2 // r.log.Debug(foo) == r.log.V(logging.DebugLevel).Info(foo)
)
