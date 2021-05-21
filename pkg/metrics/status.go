package metrics

// Status is whether a ConstraintTemplate is functioning properly.
// Reported in metrics.
type Status string

const (
	// ActiveStatus indicates a ConstraintTemplate is operating normally.
	ActiveStatus Status = "active"
	// ErrorStatus indicates there is a problem with a ConstraintTemplate.
	ErrorStatus Status = "error"
)

var (
	// AllStatuses is the set of all allowed values of Status.
	AllStatuses = []Status{ActiveStatus, ErrorStatus}
)
