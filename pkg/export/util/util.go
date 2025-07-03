package util

import "flag"

const (
	defaultConnection = "audit-connection"
	defaultChannel    = "audit-channel"
)

var (
	ExportEnabled   = flag.Bool("enable-violation-export", false, "(alpha) Enable exporting violations to external systems")
	AuditConnection = flag.String("audit-connection", defaultConnection, "(alpha) Connection name for exporting audit violation messages. Defaults to audit-connection")
	AuditChannel    = flag.String("audit-channel", defaultChannel, "(alpha) Channel name for exporting audit violation messages. Defaults to audit-channel")
)

// ExportMsg represents export message for each violation.
type ExportMsg struct {
	ID                    string            `json:"id,omitempty"`
	Details               interface{}       `json:"details,omitempty"`
	EventType             string            `json:"eventType,omitempty"`
	Group                 string            `json:"group,omitempty"`
	Version               string            `json:"version,omitempty"`
	Kind                  string            `json:"kind,omitempty"`
	Name                  string            `json:"name,omitempty"`
	Namespace             string            `json:"namespace,omitempty"`
	Message               string            `json:"message,omitempty"`
	EnforcementAction     string            `json:"enforcementAction,omitempty"`
	EnforcementActions    []string          `json:"enforcementActions,omitempty"`
	ConstraintAnnotations map[string]string `json:"constraintAnnotations,omitempty"`
	ResourceGroup         string            `json:"resourceGroup,omitempty"`
	ResourceAPIVersion    string            `json:"resourceAPIVersion,omitempty"`
	ResourceKind          string            `json:"resourceKind,omitempty"`
	ResourceNamespace     string            `json:"resourceNamespace,omitempty"`
	ResourceName          string            `json:"resourceName,omitempty"`
	ResourceLabels        map[string]string `json:"resourceLabels,omitempty"`
}

type ExportErr struct {
	Code    ExportError `json:"code"`
	Message string      `json:"message"`
}

func (e ExportErr) Error() string {
	return e.Message
}

type ExportError string

const (
	ErrConnectionNotFound ExportError = "connection_not_found"
	ErrInvalidDataType    ExportError = "invalid_data_type"
	ErrCreatingFile       ExportError = "error_creating_file"
	ErrFileDoesNotExist   ExportError = "file_does_not_exist"
	ErrMarshalingData     ExportError = "error_marshaling_data"
	ErrWritingMessage     ExportError = "error_writing_message"
	ErrCleaningUpAudit    ExportError = "error_cleaning_up_audit"
)

const (
	AuditStartedMsg   = "audit is started"
	AuditCompletedMsg = "audit is completed"
)
