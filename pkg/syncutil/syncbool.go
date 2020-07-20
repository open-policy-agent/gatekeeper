package syncutil

import "sync/atomic"

// SyncBool represents a synchronized boolean flag.
// Its methods are safe to call concurrently.
type SyncBool struct {
	val int32
}

// Set sets the values of the flag.
func (b *SyncBool) Set(v bool) {
	if v {
		atomic.StoreInt32(&b.val, 1)
	} else {
		atomic.StoreInt32(&b.val, 0)
	}
}

// Get returns the current value of the flag.
func (b *SyncBool) Get() bool {
	v := atomic.LoadInt32(&b.val)
	return v == 1
}
