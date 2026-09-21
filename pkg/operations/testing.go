package operations

import "testing"

// AssignForTest replaces the process operation set for the duration of t.
// The previous assignment is restored when the test ends.
func AssignForTest(t testing.TB, ops ...Operation) {
	t.Helper()

	next := newOperationSet()
	next.assignedOperations = make(map[Operation]bool)
	next.initialized = true
	for _, op := range ops {
		next.assignedOperations[op] = true
	}

	operationsMtx.Lock()
	prev := operations
	operations = next
	operationsMtx.Unlock()

	t.Cleanup(func() {
		operationsMtx.Lock()
		operations = prev
		operationsMtx.Unlock()
	})
}
