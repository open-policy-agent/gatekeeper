package disk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"k8s.io/client-go/util/retry"
)

type Connection struct {
	// path to store audit logs
	Path string `json:"path,omitempty"`
	// max number of audit results to store
	MaxAuditResults int `json:"maxAuditResults,omitempty"`
	// File to write audit logs
	File *os.File

	// current audit run file name
	currentAuditRun string
}

type Writer struct {
	openConnections map[string]Connection
}

const (
	maxAllowedAuditRuns = 5
)

const (
	Name = "diskwriter"
)

var Connections = &Writer{
	openConnections: make(map[string]Connection),
}

func (r *Writer) CreateConnection(_ context.Context, connectionName string, config interface{}) error {
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid config format")
	}

	path, pathOk := cfg["path"].(string)
	if !pathOk {
		return fmt.Errorf("missing or invalid values in config for connection: %s", connectionName)
	}
	var err error
	maxResults, maxResultsOk := cfg["maxAuditResults"].(float64)
	if !maxResultsOk {
		return fmt.Errorf("missing or invalid 'maxAuditResults' for connection: %s", connectionName)
	}
	if maxResults > maxAllowedAuditRuns {
		return fmt.Errorf("maxAuditResults cannot be greater than %d", maxAllowedAuditRuns)
	}

	r.openConnections[connectionName] = Connection{
		Path:            path,
		MaxAuditResults: int(maxResults),
	}
	return err
}

func (r *Writer) UpdateConnection(_ context.Context, connectionName string, config interface{}) error {
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid config format")
	}

	conn, exists := r.openConnections[connectionName]
	if !exists {
		return fmt.Errorf("connection not found: %s for Disk driver", connectionName)
	}

	var cleanUpErr error
	if path, ok := cfg["path"].(string); ok {
		if conn.Path != path {
			if err := os.RemoveAll(conn.Path); err != nil {
				cleanUpErr = fmt.Errorf("connection updated but failed to remove content form old path: %w", err)
			}
			conn.Path = path
		}
	} else {
		return fmt.Errorf("missing or invalid 'path' for connection: %s", connectionName)
	}

	if maxResults, ok := cfg["maxAuditResults"].(float64); ok {
		if maxResults > maxAllowedAuditRuns {
			return fmt.Errorf("maxAuditResults cannot be greater than %d", maxAllowedAuditRuns)
		}
		conn.MaxAuditResults = int(maxResults)
	} else {
		return fmt.Errorf("missing or invalid 'maxAuditResults' for connection: %s", connectionName)
	}

	r.openConnections[connectionName] = conn
	return cleanUpErr
}

func (r *Writer) CloseConnection(connectionName string) error {
	conn, ok := r.openConnections[connectionName]
	if !ok {
		return fmt.Errorf("connection not found: %s for disk driver", connectionName)
	}
	err := os.RemoveAll(conn.Path)
	delete(r.openConnections, connectionName)
	return err
}

func (r *Writer) Publish(_ context.Context, connectionName string, data interface{}, topic string) error {
	conn, ok := r.openConnections[connectionName]
	if !ok {
		return fmt.Errorf("connection not found: %s for disk driver", connectionName)
	}

	var violation util.ExportMsg
	if violation, ok = data.(util.ExportMsg); !ok {
		return fmt.Errorf("invalid data type, cannot convert data to exportMsg")
	}

	if violation.Message == "audit is started" {
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
		return fmt.Errorf("no file to write the violation in")
	}

	_, err = conn.File.WriteString(string(jsonData) + "\n")
	if err != nil {
		return fmt.Errorf("error writing message to disk: %w", err)
	}

	if violation.Message == "audit is completed" {
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
	conn.currentAuditRun = strings.ReplaceAll(auditID, ":", "_")

	// Ensure the directory exists
	dir := path.Join(conn.Path, topic)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	file, err := os.OpenFile(path.Join(dir, appendExtension(conn.currentAuditRun, "txt")), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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

	for {
		earliestFile, files, err := getEarliestFile(dirPath)
		if err != nil {
			return fmt.Errorf("error getting earliest file: %w", err)
		}
		if len(files) <= conn.MaxAuditResults {
			break
		}
		if err := os.Remove(earliestFile); err != nil {
			return fmt.Errorf("error removing file: %w", err)
		}
	}

	return nil
}

func getEarliestFile(dirPath string) (string, []string, error) {
	var earliestFile string
	var earliestModTime time.Time
	var files []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && (earliestFile == "" || info.ModTime().Before(earliestModTime)) {
			earliestFile = path
			earliestModTime = info.ModTime()
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", files, err
	}

	if earliestFile == "" {
		return "", files, nil
	}

	return earliestFile, files, nil
}

func appendExtension(name string, ext string) string {
	return name + "." + ext
}
