// operations stores the operations assigned to the pod via the --operation flag
// It is meant to be read-only and only set once, when flags are parsed.

package operations

import (
	"flag"
	"fmt"
	"sort"
	"strings"
)

type Operation string

const (
	Audit   = Operation("audit")
	Status  = Operation("status")
	Webhook = Operation("webhook")
)

var (
	// allOperations is a list of all possible operations that can be assigned to
	// a pod it is NOT intended to be mutated. It should be kept in alphabetical
	// order so that it can be readily compared to the results from AssignedOperations
	allOperations = []Operation{
		Audit,
		Status,
		Webhook,
	}
	operations = newOperationSet()
)

type opSet struct {
	validOperations    map[Operation]bool
	assignedOperations map[Operation]bool
	assignedStringList []string // cached serialization of the opSet
	initialized        bool
}

var _ flag.Value = &opSet{}

func newOperationSet() *opSet {
	validOps := make(map[Operation]bool)
	assignedOps := make(map[Operation]bool)
	for _, v := range allOperations {
		validOps[v] = true
		assignedOps[v] = true // default to all operations enabled
	}
	return &opSet{validOperations: validOps, assignedOperations: assignedOps}
}

func (l *opSet) String() string {
	contents := make([]string, 0)
	for k := range l.assignedOperations {
		contents = append(contents, string(k))
	}
	return fmt.Sprintf("%s", contents)
}

func (l *opSet) Set(s string) error {
	if !l.initialized {
		// When the user sets an explicit value, start fresh (no default all-values)
		l.assignedOperations = make(map[Operation]bool)
		l.initialized = true
	}
	splt := strings.Split(s, ",")
	for _, v := range splt {
		if !l.validOperations[Operation(v)] {
			return fmt.Errorf("operation %s is not a valid operation: %v", v, l.validOperations)
		}
		l.assignedOperations[Operation(v)] = true
	}
	return nil
}

func init() {
	flag.Var(operations, "operation", "The operation to be performed by this instance. e.g. audit, webhook. This flag can be declared more than once. Omitting will default to supporting all operations.")
}

// AssignedOperations returns a map of operations assigned to the pod
func AssignedOperations() map[Operation]bool {
	ret := make(map[Operation]bool)
	for k, v := range operations.assignedOperations {
		ret[k] = v
	}
	return ret
}

// IsAssigned returns true when the provided operation is assigned to the pod
func IsAssigned(op Operation) bool {
	return operations.assignedOperations[op]
}

// AssignedStringList returns a list of all operations assigned to the pod
// as a sorted list of strings
func AssignedStringList() []string {
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
