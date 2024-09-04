package syncutil

import "sync"

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

	return c.s[len(c.s)-1]
}

func (c ConcurrentErrorSlice) GetSlice() []error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var s []error
	return append(s, c.s...)
}
