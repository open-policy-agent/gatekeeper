package disk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
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

// FailedConnection wraps a Connection with retry metadata.
type FailedConnection struct {
	Connection
	FailedAt    time.Time
	RetryCount  int
	NextRetryAt time.Time
}

type Writer struct {
	mu                           sync.RWMutex
	openConnections              map[string]Connection
	closedConnections            map[string]FailedConnection
	cleanupDone                  chan struct{}
	cleanupOnce                  sync.Once
	cleanupStopped               bool
	closeAndRemoveFilesWithRetry func(conn Connection) error
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
	openConnections:   make(map[string]Connection),
	closedConnections: make(map[string]FailedConnection),
	cleanupDone:       make(chan struct{}),
	closeAndRemoveFilesWithRetry: func(conn Connection) error {
		return wait.ExponentialBackoff(retry.DefaultBackoff, func() (bool, error) {
			if conn.File != nil {
				if err := conn.unlockAndCloseFile(); err != nil {
					return false, fmt.Errorf("error closing file: %w", err)
				}
			}
			if err := os.RemoveAll(conn.Path); err != nil {
				return false, fmt.Errorf("error deleting violations stored at old path: %w", err)
			}
			return true, nil
		})
	},
}

var log = logf.Log.WithName("disk-driver").WithValues(logging.Process, "export")

func (r *Writer) CreateConnection(_ context.Context, connectionName string, config interface{}) error {
	path, maxResults, ttl, err := unmarshalConfig(config)
	if err != nil {
		return fmt.Errorf("error creating connection %s: %w", connectionName, err)
	}

	r.openConnections[connectionName] = Connection{
		Path:                path,
		MaxAuditResults:     int(maxResults),
		ClosedConnectionTTL: ttl,
	}
	return nil
}

func (r *Writer) UpdateConnection(_ context.Context, connectionName string, config interface{}) error {
	conn, exists := r.openConnections[connectionName]
	if !exists {
		return fmt.Errorf("connection %s for disk driver not found", connectionName)
	}

	path, maxResults, ttl, err := unmarshalConfig(config)
	if err != nil {
		return fmt.Errorf("error updating connection %s: %w", connectionName, err)
	}

	if conn.Path != path {
		if err := r.closeAndRemoveFilesWithRetry(conn); err != nil {
			return fmt.Errorf("error updating connection %s, %w", connectionName, err)
		}
		conn.Path = path
		conn.File = nil
	}

	conn.MaxAuditResults = int(maxResults)
	conn.ClosedConnectionTTL = ttl

	r.openConnections[connectionName] = conn
	return nil
}

func (r *Writer) CloseConnection(connectionName string) error {
	conn, ok := r.openConnections[connectionName]
	if !ok {
		return fmt.Errorf("connection %s not found for disk driver", connectionName)
	}
	defer delete(r.openConnections, connectionName)
	err := r.closeAndRemoveFilesWithRetry(conn)
	if err != nil {
		now := time.Now()
		// Store the failed connection with retry metadata with a unique key to avoid conflicts.
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
		r.cleanupOnce.Do(func() {
			go r.backgroundCleanup()
		})
	}
	return err
}

func (r *Writer) Publish(_ context.Context, connectionName string, data interface{}, topic string) error {
	conn, ok := r.openConnections[connectionName]
	if !ok {
		return fmt.Errorf("invalid connection: %s not found for disk driver", connectionName)
	}

	var violation util.ExportMsg
	if violation, ok = data.(util.ExportMsg); !ok {
		return fmt.Errorf("invalid data type: cannot convert data to exportMsg")
	}

	if violation.Message == util.AuditStartedMsg {
		err := conn.handleAuditStart(violation.ID, topic)
		if err != nil {
			return fmt.Errorf("error handling audit start: %w", err)
		}
		r.openConnections[connectionName] = conn
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling data: %w", err)
	}

	if conn.File == nil {
		return fmt.Errorf("failed to write violation: no file provided")
	}

	_, err = conn.File.WriteString(string(jsonData) + "\n")
	if err != nil {
		return fmt.Errorf("error writing message to disk: %w", err)
	}

	if violation.Message == util.AuditCompletedMsg {
		err := conn.handleAuditEnd(topic)
		if err != nil {
			return fmt.Errorf("error handling audit end: %w", err)
		}
		conn.File = nil
		conn.currentAuditRun = ""
		r.openConnections[connectionName] = conn
	}
	return nil
}

func (conn *Connection) handleAuditStart(auditID string, topic string) error {
	// Replace ':' with '_' to avoid issues with file names in windows
	conn.currentAuditRun = strings.ReplaceAll(auditID, ":", "_")

	dir := path.Join(conn.Path, topic)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Set the dir permissions to make sure reader can modify files if need be after the lock is released.
	if err := os.Chmod(dir, 0o777); err != nil {
		return fmt.Errorf("failed to set directory permissions: %w", err)
	}

	file, err := os.OpenFile(path.Join(dir, appendExtension(conn.currentAuditRun, "txt")), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	conn.File = file
	err = retry.OnError(retry.DefaultBackoff, func(_ error) bool {
		return true
	}, func() error {
		return syscall.Flock(int(conn.File.Fd()), syscall.LOCK_EX)
	})
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	log.Info("Writing latest violations in", "filename", conn.File.Name())
	return nil
}

func (conn *Connection) handleAuditEnd(topic string) error {
	if err := retry.OnError(retry.DefaultBackoff, func(_ error) bool {
		return true
	}, conn.unlockAndCloseFile); err != nil {
		return fmt.Errorf("error closing file: %w, %s", err, conn.currentAuditRun)
	}
	conn.File = nil

	readyFilePath := path.Join(conn.Path, topic, appendExtension(conn.currentAuditRun, "log"))
	if err := os.Rename(path.Join(conn.Path, topic, appendExtension(conn.currentAuditRun, "txt")), readyFilePath); err != nil {
		return fmt.Errorf("failed to rename file: %w, %s", err, conn.currentAuditRun)
	}
	// Set the file permissions to make sure reader can modify files if need be after the lock is released.
	if err := os.Chmod(readyFilePath, 0o777); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}
	log.Info("File renamed", "filename", readyFilePath)

	return conn.cleanupOldAuditFiles(topic)
}

func (conn *Connection) unlockAndCloseFile() error {
	if conn.File == nil {
		return fmt.Errorf("no file to close")
	}
	fd := int(conn.File.Fd())
	if fd < 0 {
		return fmt.Errorf("invalid file descriptor")
	}
	if err := syscall.Flock(fd, syscall.LOCK_UN); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	if err := conn.File.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}
	return nil
}

func (conn *Connection) cleanupOldAuditFiles(topic string) error {
	dirPath := path.Join(conn.Path, topic)
	files, err := getFilesSortedByModTimeAsc(dirPath)
	if err != nil {
		return fmt.Errorf("failed removing older audit files, error getting files sorted by mod time: %w", err)
	}
	var errs []error
	for i := 0; i < len(files)-conn.MaxAuditResults; i++ {
		if e := os.Remove(files[i]); e != nil {
			errs = append(errs, fmt.Errorf("error removing file: %w", e))
		}
	}

	return errors.Join(errs...)
}

func getFilesSortedByModTimeAsc(dirPath string) ([]string, error) {
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var filesInfo []fileInfo

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			filesInfo = append(filesInfo, fileInfo{path: path, modTime: info.ModTime()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(filesInfo, func(i, j int) bool {
		return filesInfo[i].modTime.Before(filesInfo[j].modTime)
	})

	var sortedFiles []string
	for _, fi := range filesInfo {
		sortedFiles = append(sortedFiles, fi.path)
	}

	return sortedFiles, nil
}

func appendExtension(name string, ext string) string {
	return name + "." + ext
}

// validatePath checks if the provided path is valid and writable.
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path must not contain '..', dir traversal is not allowed")
	}
	// validate if the path is writable
	if err := os.MkdirAll(path, 0o777); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

func unmarshalConfig(config interface{}) (string, float64, time.Duration, error) {
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return "", 0.0, 0, fmt.Errorf("invalid config format, expected map[string]interface{}")
	}

	path, pathOk := cfg[violationPath].(string)
	if !pathOk {
		return "", 0.0, 0, fmt.Errorf("missing or invalid 'path'")
	}
	if err := validatePath(path); err != nil {
		return "", 0.0, 0, fmt.Errorf("invalid path: %w", err)
	}
	maxResults, maxResultsOk := cfg[maxAuditResults].(float64)
	if !maxResultsOk {
		return "", 0.0, 0, fmt.Errorf("missing or invalid 'maxAuditResults'")
	}
	if maxResults > maxAllowedAuditRuns {
		return "", 0.0, 0, fmt.Errorf("maxAuditResults cannot be greater than the maximum allowed audit runs: %d", maxAllowedAuditRuns)
	}
	ttl := maxConnectionAge
	if ttlStr, ok := cfg["closedConnectionTTL"].(string); ok {
		duration, err := time.ParseDuration(ttlStr)
		if err != nil {
			return "", 0.0, 0, fmt.Errorf("invalid ttl format: %w", err)
		}
		ttl = duration
	}
	if ttl > maxConnectionAge {
		return "", 0.0, 0, fmt.Errorf("closedConnectionTTL %s exceeds maximum allowed: %s", ttl, maxConnectionAge)
	}
	if ttl < minConnectionAge {
		return "", 0.0, 0, fmt.Errorf("closedConnectionTTL %s is too short, must be at least 1 minute", ttl)
	}
	return path, maxResults, ttl, nil
}

// backgroundCleanup runs periodically to retry closing failed connections.
func (r *Writer) backgroundCleanup() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.retryFailedConnections()
		case <-r.cleanupDone:
			log.Info("Background cleanup stopped")
			return
		}
	}
}

// retryFailedConnections attempts to close connections that previously failed to close.
func (r *Writer) retryFailedConnections() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var toRemove []string

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

		err := r.closeAndRemoveFilesWithRetry(failedConn.Connection)
		if err == nil {
			log.Info("Successfully closed previously failed connection", "connection", name)
			toRemove = append(toRemove, name)
		} else {
			log.Info("Failed to close connection on retry", "connection", name, "error", err, "attempt", failedConn.RetryCount+1)

			delay := time.Duration(float64(baseRetryDelay) * math.Pow(retryBackoffFactor, float64(failedConn.RetryCount)))
			if maxRetryDelay > 0 && delay > maxRetryDelay {
				delay = maxRetryDelay
			}
			failedConn.RetryCount++
			// Apply jitter to the retry delay
			delay = wait.Jitter(delay, Jitter)
			failedConn.NextRetryAt = now.Add(delay)
			r.closedConnections[name] = failedConn
		}
	}

	for _, name := range toRemove {
		delete(r.closedConnections, name)
	}

	if len(r.closedConnections) > 0 {
		log.Info("Failed connections remaining", "count", len(r.closedConnections))
	} else {
		log.Info("No failed connections remaining, cleanup done")
		r.cleanupStopped = true
		close(r.cleanupDone)
	}
}
