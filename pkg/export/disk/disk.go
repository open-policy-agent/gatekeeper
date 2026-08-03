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
	// admission owns private alpha limits and resources that must close with the Connection.
	admission admissionState
}

// admissionLimits bounds the private admission spool. These are fixed alpha
// defaults rather than Connection configuration until operational data justifies
// exposing individual knobs.
type admissionLimits struct {
	maxResults     int
	maxFileBytes   int64
	maxFileRecords int64
	maxFileAge     time.Duration
	maxTotalBytes  int64
	fileTTL        time.Duration
	maxRecordBytes int64
	minFreeBytes   int64
}

// admissionState groups resources whose lifecycle is owned by the Connection.
// Pointer fields remain shared when a Connection value is copied out of a map.
type admissionState struct {
	limits       admissionLimits
	stream       *admissionStream
	cleanupTimer *time.Timer
	cleanupID    uint64
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
	// Admission rotation hooks are per Writer so tests can inject failures
	// without racing background timers through shared package state.
	renameAdmissionFile            func(oldPath, newPath string) error
	admissionRotationRetryInterval time.Duration
}

const (
	Name                           = "disk"
	maxAllowedAuditRuns            = 5
	maxAuditResults                = "maxAuditResults"
	violationPath                  = "path"
	cleanupInterval                = 2 * time.Minute
	maxRetryAttempts               = 10
	maxConnectionAge               = 10 * time.Minute
	minConnectionAge               = 1 * time.Minute
	baseRetryDelay                 = 15 * time.Second
	retryBackoffFactor             = 2.0
	maxRetryDelay                  = 10 * time.Minute
	Jitter                         = 0.1
	defaultMaxAdmissionResults     = 20
	defaultMaxAdmissionFileBytes   = 1 << 20
	defaultMaxAdmissionFileRecords = 1000
	defaultMaxAdmissionFileAge     = time.Minute
	defaultMaxAdmissionTotalBytes  = 20 << 20
	defaultAdmissionFileTTL        = 24 * time.Hour
	// Keep direct disk publishes under the same complete-record bound enforced
	// by the webhook queue so they cannot bypass its memory and spool protection.
	defaultMaxAdmissionRecordBytes = 64 << 10
	defaultMinAdmissionFreeBytes   = 16 << 20
	minAdmissionFileAge            = time.Second
	// Failed states can own live descriptors, but bounding their count prevents
	// repeated close failures from growing memory and descriptor use forever.
	maxFailedConnectionStates = 32
)

var Connections = &Writer{
	openConnections:              make(map[string]Connection),
	closedConnections:            make(map[string]FailedConnection),
	cleanupDone:                  make(chan struct{}),
	closeAndRemoveFilesWithRetry: closeAndRemoveFilesWithRetry,
}

var log = logf.Log.WithName("disk-driver").WithValues(logging.Process, "export")

func (r *Writer) CreateConnection(_ context.Context, connectionName string, config interface{}) error {
	parsed, err := parseConnectionConfig(config)
	if err != nil {
		return fmt.Errorf("error creating connection %s: %w", connectionName, err)
	}

	connLock, _ := r.acquireCurrentConnectionLock(connectionName, true)
	defer connLock.Unlock()

	r.mu.Lock()
	if _, exists := r.openConnections[connectionName]; exists {
		r.mu.Unlock()
		return fmt.Errorf("connection %s already exists for disk driver", connectionName)
	}
	if len(r.closedConnections) >= maxFailedConnectionStates {
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			delete(r.connectionLocks, connectionName)
		}
		r.mu.Unlock()
		return fmt.Errorf("maximum failed disk connection states reached: %d", maxFailedConnectionStates)
	}
	if r.pathCleanupInProgressLocked(parsed.path) {
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			delete(r.connectionLocks, connectionName)
		}
		r.mu.Unlock()
		return fmt.Errorf("error creating connection %s: path %s is being cleaned up", connectionName, parsed.path)
	}
	if r.pathOwnedByFailedConnectionLocked(parsed.path) {
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			delete(r.connectionLocks, connectionName)
		}
		r.mu.Unlock()
		return fmt.Errorf("error creating connection %s: path %s is still in use", connectionName, parsed.path)
	}
	// Register the connection before creating its directory: once it is in
	// openConnections, pathInUseLocked(path) holds, so a concurrent cleanup will
	// not remove this path while ensureDirectory runs below.
	conn := connectionFromConfig(&parsed)
	r.openConnections[connectionName] = conn
	r.mu.Unlock()

	if err := ensureDirectory(parsed.path); err != nil {
		r.mu.Lock()
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			delete(r.openConnections, connectionName)
			delete(r.connectionLocks, connectionName)
		}
		r.mu.Unlock()
		return fmt.Errorf("error creating connection %s: %w", connectionName, err)
	}
	// Recovery is prefix-filtered and therefore safe for audit-only Connections.
	// Running it at creation repairs crash remnants before new admission traffic.
	if err := conn.recoverAdmissionFiles(); err != nil {
		r.mu.Lock()
		delete(r.openConnections, connectionName)
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			delete(r.connectionLocks, connectionName)
		}
		r.mu.Unlock()
		return fmt.Errorf("recovering admission files for connection %s: %w", connectionName, err)
	}
	r.scheduleAdmissionCleanup(connectionName, &conn)
	r.mu.Lock()
	if r.connectionLockIsCurrentLocked(connectionName, connLock) {
		r.openConnections[connectionName] = conn
	}
	r.mu.Unlock()
	return nil
}

func (r *Writer) UpdateConnection(_ context.Context, connectionName string, config interface{}) error {
	parsed, err := parseConnectionConfig(config)
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
	if conn.Path != parsed.path {
		if r.pathCleanupInProgressLocked(parsed.path) {
			r.mu.Unlock()
			return fmt.Errorf("error updating connection %s: path %s is being cleaned up", connectionName, parsed.path)
		}
		if r.pathOwnedByFailedConnectionLocked(parsed.path) {
			r.mu.Unlock()
			return fmt.Errorf("error updating connection %s: path %s is still in use", connectionName, parsed.path)
		}
		// Reserve the target path before releasing r.mu so a concurrent cleanup
		// cannot remove it during the unlocked window below while ensureDirectory
		// creates it and the connection migrates onto it. The connection still
		// points at its old path, so pathInUseLocked does not yet protect the
		// target; the reservation is released once the migration commits and
		// pathInUseLocked takes over.
		r.markCleanupPathLocked(parsed.path)
		defer r.releaseCleanupPath(parsed.path)
	}
	r.mu.Unlock()
	pathChanged := conn.Path != parsed.path

	// Create the target directory only after confirming it is not mid-cleanup and
	// reserving it, so a rejected update never recreates a directory cleanup is
	// removing and an accepted update is not raced by a concurrent removal.
	if err := ensureDirectory(parsed.path); err != nil {
		return fmt.Errorf("error updating connection %s: %w", connectionName, err)
	}

	var candidate Connection
	if pathChanged {
		// Recover a fresh candidate before destroying the old path. A failed target
		// recovery must leave the currently usable connection untouched.
		candidate = connectionFromConfig(&parsed)
		if err := candidate.recoverAdmissionFiles(); err != nil {
			return fmt.Errorf("recovering admission files for connection %s: %w", connectionName, err)
		}
	}

	if pathChanged {
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
		r.scheduleAdmissionCleanup(connectionName, &candidate)
		conn = candidate
	} else {
		applyConnectionConfig(&conn, &parsed)
		if conn.admission.cleanupTimer == nil {
			if err := conn.recoverAdmissionFiles(); err != nil {
				return fmt.Errorf("recovering admission files for connection %s: %w", connectionName, err)
			}
			r.scheduleAdmissionCleanup(connectionName, &conn)
		}
	}

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

	violation, jsonData, encodeErr := decodeExportMessage(data)
	if violation.EventType == util.AdmissionViolationEventType {
		if encodeErr != nil {
			return encodeErr
		}
		if err := r.writeAdmissionRecord(ctx, connectionName, &conn, topic, jsonData); err != nil {
			r.mu.Lock()
			if r.connectionLockIsCurrentLocked(connectionName, connLock) {
				r.openConnections[connectionName] = conn
			}
			r.mu.Unlock()
			return fmt.Errorf("writing admission violation: %w", err)
		}
		r.mu.Lock()
		if r.connectionLockIsCurrentLocked(connectionName, connLock) {
			r.openConnections[connectionName] = conn
		}
		r.mu.Unlock()
		return nil
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
	if encodeErr != nil {
		return encodeErr
	}
	if conn.File == nil {
		return fmt.Errorf("failed to write violation: no file provided")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish canceled: %w", err)
	}

	_, err := conn.File.Write(append(jsonData, '\n'))
	if err != nil {
		return fmt.Errorf("error writing message to disk: %w", err)
	}

	if violation.Message == util.AuditCompletedMsg {
		connChanged = true
		if err := conn.handleAuditEnd(topic); err != nil {
			return fmt.Errorf("error handling audit end: %w", err)
		}
		conn.File = nil
		conn.currentAuditRun = ""
	}
	return nil
}

// decodeExportMessage accepts the value forms used by audit and the queued
// admission exporter. Raw messages are validated but returned byte-for-byte so
// the disk driver does not alter the encoded admission payload.
func decodeExportMessage(data interface{}) (util.ExportMsg, []byte, error) {
	var violation util.ExportMsg
	switch value := data.(type) {
	case util.ExportMsg:
		encoded, err := json.Marshal(value)
		if err != nil {
			return value, nil, fmt.Errorf("error marshaling data: %w", err)
		}
		return value, encoded, nil
	case *util.ExportMsg:
		if value == nil {
			return violation, nil, fmt.Errorf("invalid data type: nil exportMsg")
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return *value, nil, fmt.Errorf("error marshaling data: %w", err)
		}
		return *value, encoded, nil
	case json.RawMessage:
		if err := json.Unmarshal(value, &violation); err != nil {
			return violation, nil, fmt.Errorf("invalid export message: %w", err)
		}
		return violation, append([]byte(nil), value...), nil
	default:
		return violation, nil, fmt.Errorf("invalid data type: cannot convert data to exportMsg")
	}
}

// connectionFromConfig applies user-configured audit settings and initializes
// the admission spool with private safety defaults.
func connectionFromConfig(config *connectionConfig) Connection {
	conn := Connection{
		admission: admissionState{
			limits: admissionLimits{
				maxResults:     defaultMaxAdmissionResults,
				maxFileBytes:   defaultMaxAdmissionFileBytes,
				maxFileRecords: defaultMaxAdmissionFileRecords,
				maxFileAge:     defaultMaxAdmissionFileAge,
				maxTotalBytes:  defaultMaxAdmissionTotalBytes,
				fileTTL:        defaultAdmissionFileTTL,
				maxRecordBytes: defaultMaxAdmissionRecordBytes,
				minFreeBytes:   defaultMinAdmissionFreeBytes,
			},
		},
	}
	applyConnectionConfig(&conn, config)
	return conn
}

func applyConnectionConfig(conn *Connection, config *connectionConfig) {
	conn.Path = config.path
	conn.MaxAuditResults = config.maxAuditResults
	conn.ClosedConnectionTTL = config.closedConnectionTTL
}
