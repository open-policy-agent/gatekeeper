package disk

import "sync"

func (r *Writer) connectionLockLocked(connectionName string) *sync.Mutex {
	if r.connectionLocks == nil {
		r.connectionLocks = make(map[string]*sync.Mutex)
	}
	if r.connectionLocks[connectionName] == nil {
		r.connectionLocks[connectionName] = &sync.Mutex{}
	}
	return r.connectionLocks[connectionName]
}

func (r *Writer) connectionLock(connectionName string, createIfMissing bool) (*sync.Mutex, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !createIfMissing {
		if _, exists := r.openConnections[connectionName]; !exists {
			return nil, false
		}
	}
	return r.connectionLockLocked(connectionName), true
}

// acquireCurrentConnectionLock returns the current per-connection lock held.
// The caller must unlock the returned mutex.
//
// A connection lock can become stale if CloseConnection removes it after a
// caller observes the lock but before the caller acquires it. Re-checking under
// r.mu after acquiring the lock prevents callers from mutating connection state
// while holding a lock that is no longer registered for connectionName.
func (r *Writer) acquireCurrentConnectionLock(connectionName string, createIfMissing bool) (*sync.Mutex, bool) {
	for {
		connLock, exists := r.connectionLock(connectionName, createIfMissing)
		if !exists {
			return nil, false
		}

		connLock.Lock()

		r.mu.Lock()
		isCurrent := r.connectionLockIsCurrentLocked(connectionName, connLock)
		r.mu.Unlock()
		if isCurrent {
			return connLock, true
		}

		connLock.Unlock()
	}
}

func (r *Writer) connectionLockIsCurrentLocked(connectionName string, lock *sync.Mutex) bool {
	return r.connectionLocks != nil && r.connectionLocks[connectionName] == lock
}
