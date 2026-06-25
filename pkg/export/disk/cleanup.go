package disk

import (
	"fmt"
	"math"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// FailedConnection wraps a Connection with retry metadata.
type FailedConnection struct {
	Connection
	FailedAt    time.Time
	RetryCount  int
	NextRetryAt time.Time
}

func closeAndRemoveFilesWithRetry(conn *Connection) error {
	return closeAndRemoveFilesWithBackoff(conn, retry.DefaultBackoff, os.RemoveAll)
}

func closeFileWithBackoff(conn *Connection, backoff wait.Backoff) error {
	if conn.File == nil {
		return nil
	}
	var lastErr error
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		if err := conn.unlockAndCloseFile(); err != nil {
			lastErr = fmt.Errorf("error closing file: %w", err)
			return false, nil
		}
		conn.File = nil
		lastErr = nil
		return true, nil
	})
	if err != nil {
		if lastErr != nil {
			return lastErr
		}
		return err
	}
	return nil
}

func closeAndRemoveFilesWithBackoff(conn *Connection, backoff wait.Backoff, removeAll func(string) error) error {
	var lastErr error
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		if conn.File != nil {
			if err := conn.unlockAndCloseFile(); err != nil {
				lastErr = fmt.Errorf("error closing file: %w", err)
				return false, nil
			}
			conn.File = nil
		}
		if err := removeAll(conn.Path); err != nil {
			lastErr = fmt.Errorf("error deleting violations stored at old path: %w", err)
			return false, nil
		}
		lastErr = nil
		return true, nil
	})
	if err != nil {
		if lastErr != nil {
			return lastErr
		}
		return err
	}
	return nil
}

func (r *Writer) pathCleanupInProgressLocked(cleanupPath string) bool {
	_, exists := r.cleanupPaths[cleanupPath]
	return exists
}

func (r *Writer) pathInUseLocked(cleanupPath string) bool {
	for _, conn := range r.openConnections {
		if conn.Path == cleanupPath {
			return true
		}
	}
	return false
}

func (r *Writer) reserveCleanupPathLocked(cleanupPath string) bool {
	if r.pathInUseLocked(cleanupPath) || r.pathCleanupInProgressLocked(cleanupPath) {
		return false
	}
	if r.cleanupPaths == nil {
		r.cleanupPaths = make(map[string]struct{})
	}
	r.cleanupPaths[cleanupPath] = struct{}{}
	return true
}

func (r *Writer) releaseCleanupPath(cleanupPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cleanupPaths, cleanupPath)
}

// backgroundCleanup runs periodically to retry closing failed connections.
// done is captured at goroutine launch time so the goroutine selects on an
// immutable reference, avoiding a data race when CloseConnection reassigns
// r.cleanupDone.
func (r *Writer) backgroundCleanup(done <-chan struct{}) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.retryFailedConnections()
		case <-done:
			log.Info("Background cleanup stopped")
			return
		}
	}
}

// retryFailedConnections attempts to close connections that previously failed to close.
func (r *Writer) retryFailedConnections() {
	r.mu.Lock()

	now := time.Now()
	var toRemove []string
	var toRetry []string

	for name, failedConn := range r.closedConnections {
		if now.Sub(failedConn.FailedAt) > failedConn.ClosedConnectionTTL {
			log.Info("Removing expired failed connection", "connection", name, "age", now.Sub(failedConn.FailedAt))
			toRemove = append(toRemove, name)
			continue
		}

		if now.Before(failedConn.NextRetryAt) {
			continue
		}

		if failedConn.RetryCount >= maxRetryAttempts {
			log.Info("Max retry attempts exceeded for failed connection", "connection", name, "attempts", failedConn.RetryCount)
			toRemove = append(toRemove, name)
			continue
		}

		toRetry = append(toRetry, name)
	}

	for _, name := range toRemove {
		delete(r.closedConnections, name)
	}

	type retryItem struct {
		name      string
		conn      FailedConnection
		closeOnly bool
	}
	items := make([]retryItem, 0, len(toRetry))
	for _, name := range toRetry {
		failedConn := r.closedConnections[name]
		if r.pathInUseLocked(failedConn.Path) {
			items = append(items, retryItem{name: name, conn: failedConn, closeOnly: true})
			continue
		}
		if !r.reserveCleanupPathLocked(failedConn.Path) {
			continue
		}
		items = append(items, retryItem{name: name, conn: failedConn})
	}
	r.mu.Unlock()

	type retryResult struct {
		name string
		conn FailedConnection
		ok   bool
	}
	results := make([]retryResult, 0, len(items))
	for i := range items {
		var err error
		if items[i].closeOnly {
			err = closeFileWithBackoff(&items[i].conn.Connection, retry.DefaultBackoff)
		} else {
			err = r.closeAndRemoveFilesWithRetry(&items[i].conn.Connection)
			r.releaseCleanupPath(items[i].conn.Path)
		}
		if err == nil {
			log.Info("Successfully closed previously failed connection", "connection", items[i].name)
			results = append(results, retryResult{name: items[i].name, ok: true})
		} else {
			log.Info("Failed to close connection on retry", "connection", items[i].name, "error", err, "attempt", items[i].conn.RetryCount+1)
			items[i].conn.RetryCount++
			delay := time.Duration(float64(baseRetryDelay) * math.Pow(retryBackoffFactor, float64(items[i].conn.RetryCount)))
			if maxRetryDelay > 0 && delay > maxRetryDelay {
				delay = maxRetryDelay
			}
			delay = wait.Jitter(delay, Jitter)
			items[i].conn.NextRetryAt = now.Add(delay)
			results = append(results, retryResult{name: items[i].name, conn: items[i].conn})
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range results {
		if results[i].ok {
			delete(r.closedConnections, results[i].name)
		} else {
			r.closedConnections[results[i].name] = results[i].conn
		}
	}

	if len(r.closedConnections) > 0 {
		log.Info("Failed connections remaining", "count", len(r.closedConnections))
	} else {
		log.Info("No failed connections remaining, cleanup done")
		r.cleanupStopped = true
		close(r.cleanupDone)
	}
}
