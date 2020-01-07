package metrics

type Status string

const (
	ActiveStatus Status = "active"
	ErrorStatus  Status = "error"
)

var (
	AllStatuses = []Status{ActiveStatus, ErrorStatus}
)
