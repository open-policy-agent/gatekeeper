package constrainttemplate

const (
	// ErrCreateCode indicates a problem creating a ConstraintTemplate CRD.
	ErrCreateCode = "create_error"
	// ErrUpdateCode indicates a problem updating a ConstraintTemplate CRD.
	ErrUpdateCode = "update_error"
	// ErrConversionCode indicates a problem converting a ConstraintTemplate CRD.
	ErrConversionCode = "conversion_error"
	// ErrIngestCode indicates a problem ingesting a ConstraintTemplate Rego code.
	ErrIngestCode = "ingest_error"
	// ErrParseCode indicates a problem parsing a ConstraintTemplate.
	ErrParseCode = "parse_error"
)

const (
	// ErrGenerateVAPState indicates a problem generating a VAP.
	ErrGenerateVAPState = "error"
	// GeneratedVAPState indicates a VAP was generated.
	GeneratedVAPState = "generated"
	// ErrOperationMismatchCode indicates operations mismatch between webhook and constraint template.
	ErrOperationMismatchCode = "operation_mismatch_warning"
)
