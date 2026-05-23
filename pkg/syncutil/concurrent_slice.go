package syncutil

import "sync"

// ConcurrentErrorSlice stores non-nil errors in a thread-safe slice.
// Append ignores nil errors so Last() can safely return nil only when the slice is empty.
type ConcurrentErrorSlice struct {
	s  []error
	mu *sync.RWMutex
}

func NewConcurrentErrorSlice() ConcurrentErrorSlice {
	return ConcurrentErrorSlice{
		s:  make([]error, 0),
		mu: &sync.RWMutex{},
	}
}

func (c ConcurrentErrorSlice) Append(e error) ConcurrentErrorSlice {
	if e == nil {
		return c
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	return ConcurrentErrorSlice{
		s:  append(c.s, e),
		mu: c.mu,
	}
}

func (c ConcurrentErrorSlice) Last() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.s) == 0 {
		return nil
	}
	return c.s[len(c.s)-1]
}

func (c ConcurrentErrorSlice) GetSlice() []error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var s []error
	return append(s, c.s...)
}
