package operations

import (
	"flag"
	"fmt"
	"sort"
	"sync"
)

type Operation string

const (
	Audit   = Operation("audit")
	Status  = Operation("status")
	Webhook = Operation("webhook")
)

var (
	// AllOperations is a list of all possible operations that can be assigned to
	// a pod it is NOT intended to be mutated. It should be kept in alphabetical
	// order so that it can be readily compared to the results from AssignedOperations
	AllOperations = []Operation{
		Audit,
		Status,
		Webhook,
	}
	operations = newOperationSet()
	initOnce   = sync.Once{}
)

type opSet struct {
	validOperations    map[Operation]bool
	assignedOperations map[Operation]bool
	assignedStringList []string
}

var _ flag.Value = &opSet{}

func newOperationSet() *opSet {
	validOps := make(map[Operation]bool)
	for _, v := range AllOperations {
		validOps[v] = true
	}
	return &opSet{validOperations: validOps, assignedOperations: make(map[Operation]bool)}
}

func (l *opSet) String() string {
	contents := make([]string, 0)
	for k := range l.assignedOperations {
		contents = append(contents, string(k))
	}
	return fmt.Sprintf("%s", contents)
}

func (l *opSet) Set(s string) error {
	if !l.validOperations[Operation(s)] {
		return fmt.Errorf("Operation %s is not a valid operation: %v", s, l.validOperations)
	}
	l.assignedOperations[Operation(s)] = true
	return nil
}

func init() {
	flag.Var(operations, "operation", "The operation to be performed by this instance. e.g. audit, webhook. This flag can be declared more than once. Omitting will default to supporting all operations.")
}

// defaulting sets default if --operation is not provided
func defaulting() {
	if len(operations.assignedOperations) == 0 {
		operations.assignedOperations = operations.validOperations
	}
}

// AssignedOperations returns a map of operations assigned to the pod
func AssignedOperations() map[Operation]bool {
	initOnce.Do(defaulting)
	ret := make(map[Operation]bool)
	for k, v := range operations.assignedOperations {
		ret[k] = v
	}
	return ret
}

// IsAssigned returns true when the provided operation is assigned to the pod
func IsAssigned(op Operation) bool {
	initOnce.Do(defaulting)
	return operations.assignedOperations[op]
}

// AssignedStringList returns a list of all operations assigned to the pod
// as a sorted list of strings
func AssignedStringList() []string {
	initOnce.Do(defaulting)
	if operations.assignedStringList != nil {
		return operations.assignedStringList
	}
	var ret []string
	for k := range operations.assignedOperations {
		ret = append(ret, string(k))
	}
	sort.Strings(ret)
	operations.assignedStringList = ret
	return operations.assignedStringList
}
