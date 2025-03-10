// operations stores the operations assigned to the pod via the --operation flag
// It is meant to be read-only and only set once, when flags are parsed.

package operations

import (
	"flag"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Operation string

// All defined Operations.
const (
	Audit              = Operation("audit")
	MutationController = Operation("mutation-controller")
	MutationStatus     = Operation("mutation-status")
	MutationWebhook    = Operation("mutation-webhook")
	Status             = Operation("status")
	Webhook            = Operation("webhook")
	Generate           = Operation("generate")
)

var (
	// allOperations is a list of all possible Operations that can be assigned to
	// a pod. It is NOT intended to be mutated.
	allOperations = []Operation{
		Audit,
		Generate,
		MutationController,
		MutationStatus,
		MutationWebhook,
		Status,
		Webhook,
	}

	operationsMtx sync.RWMutex
	operations    = newOperationSet()
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

// IsAssigned returns true when the provided operation is assigned to the pod.
func IsAssigned(op Operation) bool {
	operationsMtx.RLock()
	defer operationsMtx.RUnlock()

	return operations.assignedOperations[op]
}

// AssignedStringList returns a list of all operations assigned to the pod
// as a sorted list of strings.
func AssignedStringList() []string {
	// Use a read lock so we can exit early without potentially having multiple
	// threads try to write this simultaneously.
	operationsMtx.RLock()
	gotList := operations.assignedStringList
	operationsMtx.RUnlock()
	if gotList != nil {
		return gotList
	}

	operationsMtx.Lock()
	defer operationsMtx.Unlock()
	// Verify the list hasn't been set since we last checked.
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

// HasValidationOperations returns `true` if there
// are any operations that would require a constraint or template controller
// or a sync controller.
func HasValidationOperations() bool {
	return IsAssigned(Audit) || IsAssigned(Status) || IsAssigned(Webhook)
}
