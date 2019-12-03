package util

import (
	"sync"
)

type ConstraintsCache struct {
	CacheMux sync.RWMutex
	Cache    map[string]Tags
}

type Tags struct {
	EnforcementAction EnforcementAction
	Status            Status
}

var KnownConstraintStatus = []Status{ActiveStatus, ErrorStatus}

type Status string

const (
	ActiveStatus Status = "active"
	ErrorStatus  Status = "error"
)
