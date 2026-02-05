package metrics

// VAPStatus represents the status of a VAP resource.
// Reported in metrics.
type VAPStatus string

const (
	// VAPStatusActive indicates a VAP is operating normally.
	VAPStatusActive VAPStatus = "active"
	// VAPStatusError indicates there is a problem with a VAP.
	VAPStatusError VAPStatus = "error"
)

// AllVAPStatuses is the set of all allowed values of VAPStatus.
var AllVAPStatuses = []VAPStatus{VAPStatusActive, VAPStatusError}
