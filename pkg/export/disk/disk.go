package disk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type Connection struct {
	// path to store audit logs
	Path string `json:"path,omitempty"`
	// max number of audit results to store
	MaxAuditResults int `json:"maxAuditResults,omitempty"`
	// ClosedConnectionTTL specifies how long a failed connection remains
	// in the cleanup queue before being permanently removed (not retried).
	// This prevents memory leaks from accumulating failed connections.
	ClosedConnectionTTL time.Duration `json:"closedConnectionTTL,omitempty"`
	// File to write audit logs
	File *os.File

	// current audit run file name
	currentAuditRun string
}
type Writer struct {
	mu                           sync.Mutex
	openConnections              map[string]Connection
	connectionLocks              map[string]*sync.Mutex
	cleanupPaths                 map[string]struct{}
	closedConnections            map[string]FailedConnection
	cleanupDone                  chan struct{}
	cleanupOnce                  sync.Once
	cleanupStopped               bool
	closeAndRemoveFilesWithRetry func(conn *Connection) error
}

const (
	Name                = "disk"
	maxAllowedAuditRuns = 5
	maxAuditResults     = "maxAuditResults"
	violationPath       = "path"
	cleanupInterval     = 2 * time.Minute
	maxRetryAttempts    = 10
	maxConnectionAge    = 10 * time.Minute
	minConnectionAge    = 1 * time.Minute
	baseRetryDelay      = 15 * time.Second
	retryBackoffFactor  = 2.0
	maxRetryDelay       = 10 * time.Minute
	Jitter              = 0.1
)

var Connections = &Writer{
	openConnections:              make(map[string]Connection),
	closedConnections:            make(map[string]FailedConnection),
	cleanupDone:                  make(chan struct{}),
	closeAndRemoveFilesWithRetry: closeAndRemoveFilesWithRetry,
}

var log = logf.Log.WithName("disk-driver").WithValues(logging.Process, "export")

func (r *Writer) CreateConnection(_ context.Context, connectionName string, config interface{}) error {
	path, maxResults, ttl, err := unmarshalConfig(config)
	if err != nil {
		return fmt.Errorf("error creating connection %s: %w", connectionName, err)
	}

	connLock, _ := r.acquireCurrentConnectionLock(connectionName, true)
	defer connLock.Unlock()

	r.mu.Lock()
	if r.pathCleanupInProgressLocked(path) {
		r.mu.Unlock()
		return fmt.Errorf("error creating connection %s: path %s is being cleaned up", connectionName, path)
	}
	r.openConnections[connectionName] = Connection{
		Path:                path,
		MaxAuditResults:     int(maxResults),
		ClosedConnectionTTL: ttl,
	}
	r.mu.Unlock()
	return nil
}

func (r *Writer) UpdateConnection(_ context.Context, connectionName string, config interface{}) error {
	path, maxResults, ttl, err := unmarshalConfig(config)
	if err != nil {
		return fmt.Errorf("error updating connection %s: %w", connectionName, err)
	}

	connLock, exists := r.acquireCurrentConnectionLock(connectionName, false)
	if !exists {
		return fmt.Errorf("connection %s for disk driver not found", connectionName)
	}
	defer connLock.Unlock()

	r.mu.Lock()
	conn, exists := r.openConnections[connectionName]
	if !exists || !r.connectionLockIsCurrentLocked(connectionName, connLock) {
		r.mu.Unlock()
		return fmt.Errorf("connection %s for disk driver not found", connectionName)
	}
	if conn.Path != path && r.pathCleanupInProgressLocked(path) {
		r.mu.Unlock()
		return fmt.Errorf("error updating connection %s: path %s is being cleaned up", connectionName, path)
	}
	r.mu.Unlock()

	if conn.Path != path {
		// Keep the old connection visible while cleanup runs so concurrent callers
		// can find the current lock and wait instead of observing a transient miss.
		if err := r.closeAndCleanupConnection(connectionName, &conn); err != nil {
			r.mu.Lock()
			if r.connectionLockIsCurrentLocked(connectionName, connLock) {
				r.openConnections[connectionName] = conn
			}
			r.mu.Unlock()
			return fmt.Errorf("error updating connection %s, %w", connectionName, err)
		}
		conn.Path = path
		conn.File = nil
	}

	conn.MaxAuditResults = int(maxResults)
	conn.ClosedConnectionTTL = ttl

	r.mu.Lock()
	r.openConnections[connectionName] = conn
	r.mu.Unlock()
	return nil
}

func (r *Writer) CloseConnection(connectionName string) error {
	connLock, exists := r.acquireCurrentConnectionLock(connectionName, false)
	if !exists {
		return fmt.Errorf("connection %s not found for disk driver", connectionName)
	}
	defer connLock.Unlock()

	r.mu.Lock()
	conn, ok := r.openConnections[connectionName]
	if !ok || !r.connectionLockIsCurrentLocked(connectionName, connLock) {
		r.mu.Unlock()
		return fmt.Errorf("connection %s not found for disk driver", connectionName)
	}
	delete(r.openConnections, connectionName)
	r.mu.Unlock()

	err := r.closeAndCleanupConnection(connectionName, &conn)
	if err != nil {
		now := time.Now()
		r.mu.Lock()
		// Store the failed connection with retry metadata under a timestamped key to avoid replacing prior failures.
		r.closedConnections[connectionName+now.String()] = FailedConnection{
			Connection:  conn,
			FailedAt:    now,
			RetryCount:  0,
			NextRetryAt: now.Add(baseRetryDelay),
		}
		if r.cleanupStopped {
			r.cleanupOnce = sync.Once{}
			r.cleanupDone = make(chan struct{})
			r.cleanupStopped = false
		}
		done := r.cleanupDone
		r.cleanupOnce.Do(func() {
			go r.backgroundCleanup(done)
		})
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			delete(r.connectionLocks, connectionName)
		}
		r.mu.Unlock()
		return err
	}

	r.mu.Lock()
	if r.connectionLockIsCurrentLocked(connectionName, connLock) {
		delete(r.connectionLocks, connectionName)
	}
	r.mu.Unlock()
	return nil
}

func (r *Writer) Publish(ctx context.Context, connectionName string, data interface{}, topic string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish canceled: %w", err)
	}

	connLock, exists := r.acquireCurrentConnectionLock(connectionName, false)
	if !exists {
		return fmt.Errorf("invalid connection: %s not found for disk driver", connectionName)
	}
	defer connLock.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish canceled: %w", err)
	}

	r.mu.Lock()
	conn, ok := r.openConnections[connectionName]
	if !ok || !r.connectionLockIsCurrentLocked(connectionName, connLock) {
		r.mu.Unlock()
		return fmt.Errorf("invalid connection: %s not found for disk driver", connectionName)
	}
	r.mu.Unlock()

	var violation util.ExportMsg
	if violation, ok = data.(util.ExportMsg); !ok {
		return fmt.Errorf("invalid data type: cannot convert data to exportMsg")
	}
	connChanged := false
	defer func() {
		if !connChanged {
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			r.openConnections[connectionName] = conn
		}
	}()

	if violation.Message == util.AuditStartedMsg {
		err := conn.handleAuditStart(violation.ID, topic)
		if err != nil {
			return fmt.Errorf("error handling audit start: %w", err)
		}
		connChanged = true
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling data: %w", err)
	}

	if conn.File == nil {
		return fmt.Errorf("failed to write violation: no file provided")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish canceled: %w", err)
	}

	_, err = conn.File.Write(append(jsonData, '\n'))
	if err != nil {
		return fmt.Errorf("error writing message to disk: %w", err)
	}

	if violation.Message == util.AuditCompletedMsg {
		connChanged = true
		err := conn.handleAuditEnd(topic)
		if err != nil {
			return fmt.Errorf("error handling audit end: %w", err)
		}
		conn.File = nil
		conn.currentAuditRun = ""
	}
	return nil
}
