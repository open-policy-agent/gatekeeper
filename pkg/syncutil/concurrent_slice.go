package syncutil

import "sync"

// ConcurrentErrorSlice stores non-nil errors in a thread-safe slice.
// The zero value is safe to use, and Append ignores nil errors so Last() can safely return nil only when the slice is empty.
type ConcurrentErrorSlice struct {
	s  []error
	mu sync.RWMutex
}

func NewConcurrentErrorSlice() ConcurrentErrorSlice {
	return ConcurrentErrorSlice{
		s: make([]error, 0),
	}
}

func (c *ConcurrentErrorSlice) Append(e error) {
	if e == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.s = append(c.s, e)
}

func (c *ConcurrentErrorSlice) Last() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.s) == 0 {
		return nil
	}
	return c.s[len(c.s)-1]
}

func (c *ConcurrentErrorSlice) GetSlice() []error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var s []error
	return append(s, c.s...)
}
