package disk

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

func TestCreateConnection(t *testing.T) {
	writer := &Writer{
		openConnections: make(map[string]Connection),
	}
	tmpPath := t.TempDir()
	tests := []struct {
		name           string
		connectionName string
		config         interface{}
		err            error
		expectError    bool
	}{
		{
			name:           "Valid config",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":            tmpPath,
				"maxAuditResults": 3.0,
			},
			expectError: false,
		},
		{
			name:           "Invalid config format",
			connectionName: "conn2",
			config: map[int]interface{}{
				1: "test",
			},
			err:         fmt.Errorf("error creating connection conn2: invalid config format, expected map[string]interface{}"),
			expectError: true,
		},
		{
			name:           "Missing path",
			connectionName: "conn3",
			config: map[string]interface{}{
				"maxAuditResults": 10.0,
			},
			err:         fmt.Errorf("error creating connection conn3: missing or invalid 'path'"),
			expectError: true,
		},
		{
			name:           "Missing maxAuditResults",
			connectionName: "conn4",
			config: map[string]interface{}{
				"path": tmpPath,
			},
			err:         fmt.Errorf("error creating connection conn4: missing or invalid 'maxAuditResults'"),
			expectError: true,
		},
		{
			name:           "Exceeding maxAuditResults",
			connectionName: "conn4",
			config: map[string]interface{}{
				"path":            tmpPath,
				"maxAuditResults": 10.0,
			},
			err:         fmt.Errorf("error creating connection conn4: maxAuditResults cannot be greater than the maximum allowed audit runs: 5"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writer.CreateConnection(context.Background(), tt.connectionName, tt.config)
			if tt.expectError && tt.err.Error() != err.Error() {
				t.Errorf("CreateConnection() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				conn, exists := writer.openConnections[tt.connectionName]
				if !exists {
					t.Errorf("Connection %s was not created", tt.connectionName)
				}
				path, pathOk := tt.config.(map[string]interface{})["path"].(string)
				if !pathOk {
					t.Errorf("Failed to get path from config")
				}
				if conn.Path != path {
					t.Errorf("Expected path %s, got %s", path, conn.Path)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Errorf("failed to stat path: %s", err.Error())
				}
				if !info.IsDir() {
					t.Errorf("path is not a directory")
				}
				maxAuditResults, maxResultsOk := tt.config.(map[string]interface{})["maxAuditResults"].(float64)
				if !maxResultsOk {
					t.Errorf("Failed to get maxAuditResults from config")
				}
				if conn.MaxAuditResults != int(maxAuditResults) {
					t.Errorf("Expected maxAuditResults %d, got %d", int(maxAuditResults), conn.MaxAuditResults)
				}
			}
		})
	}
}

func TestUpdateConnection(t *testing.T) {
	writer := &Writer{
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
	tmpPath := t.TempDir()
	file, err := os.CreateTemp(tmpPath, "testfile")
	if err != nil {
		t.Errorf("Failed to create temp file: %v", err)
	}

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
	if err != nil {
		t.Errorf("Failed to lock file: %v", err)
	}

	writer.openConnections["conn1"] = Connection{
		Path:            tmpPath,
		MaxAuditResults: 3,
		File:            file,
	}

	tests := []struct {
		name           string
		connectionName string
		config         interface{}
		expectError    bool
		err            error
	}{
		{
			name:           "Valid update",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":            t.TempDir(),
				"maxAuditResults": 4.0,
			},
			expectError: false,
			err:         nil,
		},
		{
			name:           "Invalid config format",
			connectionName: "conn1",
			config: map[int]interface{}{
				1: "test",
			},
			expectError: true,
			err:         fmt.Errorf("error updating connection conn1: invalid config format, expected map[string]interface{}"),
		},
		{
			name:           "Connection not found",
			connectionName: "conn2",
			config: map[string]interface{}{
				"path":            t.TempDir(),
				"maxAuditResults": 2.0,
			},
			expectError: true,
			err:         fmt.Errorf("connection conn2 for disk driver not found"),
		},
		{
			name:           "Missing path",
			connectionName: "conn1",
			config: map[string]interface{}{
				"maxAuditResults": 2.0,
			},
			expectError: true,
			err:         fmt.Errorf("error updating connection conn1: missing or invalid 'path'"),
		},
		{
			name:           "Missing maxAuditResults",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path": t.TempDir(),
			},
			expectError: true,
			err:         fmt.Errorf("error updating connection conn1: missing or invalid 'maxAuditResults'"),
		},
		{
			name:           "Exceeding maxAuditResults",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":            t.TempDir(),
				"maxAuditResults": 10.0,
			},
			expectError: true,
			err:         fmt.Errorf("error updating connection conn1: maxAuditResults cannot be greater than the maximum allowed audit runs: 5"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writer.UpdateConnection(context.Background(), tt.connectionName, tt.config)
			if tt.expectError && tt.err.Error() != err.Error() {
				t.Errorf("UpdateConnection() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				conn, exists := writer.openConnections[tt.connectionName]
				if !exists {
					t.Errorf("Connection %s was not found", tt.connectionName)
				}
				path, pathOk := tt.config.(map[string]interface{})["path"].(string)
				if !pathOk {
					t.Errorf("Failed to get path from config")
				}
				if conn.Path != path {
					t.Errorf("Expected path %s, got %s", path, conn.Path)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Errorf("failed to stat path: %s", err.Error())
				}
				if !info.IsDir() {
					t.Errorf("path is not a directory")
				}
				maxAuditResults, maxResultsOk := tt.config.(map[string]interface{})["maxAuditResults"].(float64)
				if !maxResultsOk {
					t.Errorf("Failed to get maxAuditResults from config")
				}
				if conn.MaxAuditResults != int(maxAuditResults) {
					t.Errorf("Expected maxAuditResults %d, got %d", int(maxAuditResults), conn.MaxAuditResults)
				}
			}
		})
	}
}

func TestCloseConnection(t *testing.T) {
	writer := &Writer{
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

	tests := []struct {
		name              string
		connectionName    string
		setup             func(writer *Writer) error
		expectError       bool
		expectClosedEntry bool
	}{
		{
			name:           "Valid close",
			connectionName: "conn1",
			setup: func(writer *Writer) error {
				writer.openConnections["conn1"] = Connection{
					Path:            t.TempDir(),
					MaxAuditResults: 10,
				}
				return nil
			},
			expectError:       false,
			expectClosedEntry: false,
		},
		{
			name:              "Connection not found",
			connectionName:    "conn2",
			setup:             nil,
			expectError:       true,
			expectClosedEntry: false,
		},
		{
			name:           "Valid close with open and locked file",
			connectionName: "conn3",
			setup: func(writer *Writer) error {
				d := t.TempDir()
				if err := os.MkdirAll(d, 0o755); err != nil {
					return err
				}
				file, err := os.CreateTemp(d, "testfile")
				if err != nil {
					return err
				}
				writer.openConnections["conn3"] = Connection{
					Path:            d,
					MaxAuditResults: 10,
					File:            file,
				}
				return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
			},
			expectError:       false,
			expectClosedEntry: false,
		},
		{
			name:           "Close connection with failing closeAndRemoveFilesWithRetry",
			connectionName: "failing-conn",
			setup: func(writer *Writer) error {
				tmpDir := t.TempDir()
				writer.openConnections["failing-conn"] = Connection{
					Path: tmpDir,
				}
				writer.closeAndRemoveFilesWithRetry = func(_ Connection) error {
					return fmt.Errorf("forced failure")
				}
				return nil
			},
			expectError:       true,
			expectClosedEntry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				if err := tt.setup(writer); err != nil {
					t.Errorf("Setup failed: %v", err)
				}
			}
			err := writer.CloseConnection(tt.connectionName)
			if (err != nil) != tt.expectError {
				t.Errorf("CloseConnection() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				_, exists := writer.openConnections[tt.connectionName]
				if exists {
					t.Errorf("Connection %s was not closed", tt.connectionName)
				}
			}

			if _, exists := writer.openConnections[tt.connectionName]; exists {
				t.Errorf("connection %s still exists in openConnections after CloseConnection", tt.connectionName)
			}

			if tt.expectClosedEntry {
				if len(writer.closedConnections) == 0 {
					t.Errorf("expected a failed connection to be in closedConnections, but it was not")
				}
			} else {
				if _, exists := writer.closedConnections[tt.connectionName]; exists {
					t.Errorf("did not expect connection %s to be in closedConnections, but it was", tt.connectionName)
				}
			}
		})
	}
}

func TestPublish(t *testing.T) {
	writer := &Writer{
		openConnections: make(map[string]Connection),
	}

	writer.openConnections["conn1"] = Connection{
		Path:            t.TempDir(),
		MaxAuditResults: 1,
	}

	tests := []struct {
		name           string
		connectionName string
		data           interface{}
		topic          string
		expectError    bool
	}{
		{
			name:           "Valid publish - audit started",
			connectionName: "conn1",
			data: util.ExportMsg{
				ID:      "audit1",
				Message: "audit is started",
			},
			topic:       "topic1",
			expectError: false,
		},
		{
			name:           "Valid publish - audit in progress",
			connectionName: "conn1",
			data: util.ExportMsg{
				ID:      "audit1",
				Message: "audit is in progress",
			},
			topic:       "topic1",
			expectError: false,
		},
		{
			name:           "Valid publish - audit completed",
			connectionName: "conn1",
			data: util.ExportMsg{
				ID:      "audit1",
				Message: "audit is completed",
			},
			topic:       "topic1",
			expectError: false,
		},
		{
			name:           "Invalid data type",
			connectionName: "conn1",
			data:           "invalid data",
			topic:          "topic1",
			expectError:    true,
		},
		{
			name:           "Connection not found",
			connectionName: "conn2",
			data: util.ExportMsg{
				ID:      "audit1",
				Message: "audit is started",
			},
			topic:       "topic1",
			expectError: true,
		},
		{
			name:           "Valid publish - 2nd audit started",
			connectionName: "conn1",
			data: util.ExportMsg{
				ID:      "audit2",
				Message: "audit is started",
			},
			topic:       "topic1",
			expectError: false,
		},
		{
			name:           "Valid publish - 2nd audit in progress",
			connectionName: "conn1",
			data: util.ExportMsg{
				ID:      "audit2",
				Message: "audit is in progress",
			},
			topic:       "topic1",
			expectError: false,
		},
		{
			name:           "Valid publish - 2nd audit completed",
			connectionName: "conn1",
			data: util.ExportMsg{
				ID:      "audit2",
				Message: "audit is completed",
			},
			topic:       "topic1",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writer.Publish(context.Background(), tt.connectionName, tt.data, tt.topic)
			if (err != nil) != tt.expectError {
				t.Errorf("Publish() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				files, err := listFiles(path.Join(writer.openConnections[tt.connectionName].Path, tt.topic))
				if err != nil {
					t.Errorf("Failed to list files: %v", err)
				}
				msg, ok := tt.data.(util.ExportMsg)
				if !ok {
					t.Errorf("Failed to convert data to ExportMsg")
				}
				if msg.Message == "audit is started" {
					if len(files) > 2 {
						t.Errorf("Expected <= 2 file, got %d, %v", len(files), files)
					}
					if slices.Contains(files, writer.openConnections[tt.connectionName].currentAuditRun+".txt") {
						t.Errorf("Expected file %s to exist, but it does not", writer.openConnections[tt.connectionName].currentAuditRun+".txt")
					}
				}
				if msg.Message == "audit is completed" {
					if len(files) != 1 {
						t.Errorf("Expected 1 file, got %d, %v", len(files), files)
					}
					if slices.Contains(files, msg.ID+".log") {
						t.Errorf("Expected file %s to exist, but it does not, files: %v", msg.ID+".log", files)
					}
					content, err := os.ReadFile(files[0])
					if err != nil {
						t.Errorf("Failed to read file: %v", err)
					}
					for _, msg := range []string{"audit is started", "audit is in progress", "audit is completed"} {
						if !strings.Contains(string(content), msg) {
							t.Errorf("Expected message %q in file %s, but it was not found", msg, files[0])
						}
					}
				}
			}
		})
	}
}

func TestHandleAuditStart(t *testing.T) {
	tests := []struct {
		name        string
		connection  Connection
		auditID     string
		topic       string
		expectError bool
	}{
		{
			name: "Valid audit start",
			connection: Connection{
				Path: t.TempDir(),
			},
			auditID:     "audit1",
			topic:       "topic1",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.connection.handleAuditStart(tt.auditID, tt.topic)
			if (err != nil) != tt.expectError {
				t.Errorf("handleAuditStart() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				expectedFileName := path.Join(tt.connection.Path, tt.topic, tt.auditID+".txt")
				if tt.connection.currentAuditRun != tt.auditID {
					t.Errorf("Expected currentAuditRun %s, got %s", tt.auditID, tt.connection.currentAuditRun)
				}
				if tt.connection.File == nil {
					t.Errorf("Expected file to be opened, but it is nil")
				} else {
					if tt.connection.File.Name() != expectedFileName {
						t.Errorf("Expected file name %s, got %s", expectedFileName, tt.connection.File.Name())
					}
					tt.connection.File.Close()
				}
			}
		})
	}
}

func TestHandleAuditEnd(t *testing.T) {
	tests := []struct {
		name         string
		connection   Connection
		topic        string
		setup        func(conn *Connection) error
		expectError  bool
		expectedFile string
	}{
		{
			name: "Valid audit end",
			connection: Connection{
				Path:            t.TempDir(),
				currentAuditRun: "audit1",
			},
			topic: "topic1",
			setup: func(conn *Connection) error {
				dir := path.Join(conn.Path, "topic1")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				file, err := os.Create(path.Join(dir, conn.currentAuditRun+".txt"))
				if err != nil {
					return err
				}
				conn.File = file
				return nil
			},
			expectError: false,
		},
		{
			name: "Cleanup old audit files error",
			connection: Connection{
				Path:            t.TempDir(),
				currentAuditRun: "audit1",
				MaxAuditResults: 1,
			},
			topic: "topic1",
			setup: func(conn *Connection) error {
				dir := path.Join(conn.Path, "topic1")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				if _, err := os.Create(path.Join(dir, "extra_audit.log")); err != nil {
					return err
				}
				file, err := os.Create(path.Join(dir, conn.currentAuditRun+".txt"))
				if err != nil {
					return err
				}
				conn.File = file
				return nil
			},
			expectError:  false,
			expectedFile: "audit1.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				if err := tt.setup(&tt.connection); err != nil {
					t.Errorf("Setup failed: %v", err)
				}
			}
			err := tt.connection.handleAuditEnd(tt.topic)
			if (err != nil) != tt.expectError {
				t.Errorf("handleAuditEnd() error = %v, expectError %v", err, tt.expectError)
			}

			if !tt.expectError {
				files, err := listFiles(path.Join(tt.connection.Path, tt.topic))
				if err != nil {
					t.Errorf("Failed to list files: %v", err)
				}
				if slices.Contains(files, tt.expectedFile) {
					t.Errorf("Expected file %s to exist, but it does not. Files: %v", tt.expectedFile, files)
				}
			}
		})
	}
}

func listFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

func TestUnlockAndCloseFile(t *testing.T) {
	tests := []struct {
		name        string
		connection  Connection
		setup       func(conn *Connection) error
		expectError bool
	}{
		{
			name: "Valid unlock and close",
			connection: Connection{
				Path: t.TempDir(),
			},
			setup: func(conn *Connection) error {
				if err := os.MkdirAll(conn.Path, 0o755); err != nil {
					return err
				}
				file, err := os.CreateTemp(conn.Path, "testfile")
				if err != nil {
					return err
				}
				conn.File = file
				return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
			},
			expectError: false,
		},
		{
			name: "No file to close",
			connection: Connection{
				Path: t.TempDir(),
			},
			setup:       nil,
			expectError: true,
		},
		{
			name: "Invalid file descriptor",
			connection: Connection{
				Path: t.TempDir(),
			},
			setup: func(conn *Connection) error {
				file, err := os.CreateTemp(conn.Path, "testfile")
				if err != nil {
					return err
				}
				conn.File = file
				file.Close()
				return nil
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				if err := tt.setup(&tt.connection); err != nil {
					t.Errorf("Setup failed: %v", err)
				}
			}
			err := tt.connection.unlockAndCloseFile()
			if (err != nil) != tt.expectError {
				t.Errorf("unlockAndCloseFile() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestCleanupOldAuditFiles(t *testing.T) {
	tests := []struct {
		name          string
		connection    Connection
		topic         string
		setup         func(conn *Connection) error
		expectError   bool
		expectedFiles int
	}{
		{
			name: "No files to clean up",
			connection: Connection{
				Path:            t.TempDir(),
				MaxAuditResults: 5,
			},
			topic: "topic1",
			setup: func(conn *Connection) error {
				return os.MkdirAll(path.Join(conn.Path, "topic1"), 0o755)
			},
			expectError:   false,
			expectedFiles: 0,
		},
		{
			name: "Files within limit",
			connection: Connection{
				Path:            t.TempDir(),
				MaxAuditResults: 5,
			},
			topic: "topic1",
			setup: func(conn *Connection) error {
				dir := path.Join(conn.Path, "topic1")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				for i := 0; i < 3; i++ {
					if _, err := os.Create(path.Join(dir, fmt.Sprintf("audit%d.txt", i))); err != nil {
						return err
					}
				}
				return nil
			},
			expectError:   false,
			expectedFiles: 3,
		},
		{
			name: "Files exceeding limit",
			connection: Connection{
				Path:            t.TempDir(),
				MaxAuditResults: 2,
			},
			topic: "topic1",
			setup: func(conn *Connection) error {
				dir := path.Join(conn.Path, "topic1")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				for i := 0; i < 4; i++ {
					if _, err := os.Create(path.Join(dir, fmt.Sprintf("audit%d.txt", i))); err != nil {
						return err
					}
				}
				return nil
			},
			expectError:   false,
			expectedFiles: 2,
		},
		{
			name: "Error getting earliest file",
			connection: Connection{
				Path:            t.TempDir(),
				MaxAuditResults: 2,
			},
			topic:         "topic1",
			setup:         nil,
			expectError:   true,
			expectedFiles: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				if err := tt.setup(&tt.connection); err != nil {
					t.Errorf("Setup failed: %v", err)
				}
			}
			err := tt.connection.cleanupOldAuditFiles(tt.topic)
			if (err != nil) != tt.expectError {
				t.Errorf("cleanupOldAuditFiles() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				dir := path.Join(tt.connection.Path, tt.topic)
				files, err := os.ReadDir(dir)
				if err != nil {
					t.Errorf("Failed to read directory: %v", err)
				}
				if len(files) != tt.expectedFiles {
					t.Errorf("Expected %d files, got %d", tt.expectedFiles, len(files))
				}
			}
		})
	}
}

func TestGetFilesSortedByModTimeAsc(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(dir string) error
		expectedFile  string
		expectedFiles int
		expectError   bool
	}{
		{
			name: "No files in directory",
			setup: func(_ string) error {
				return nil
			},
			expectedFile:  "",
			expectedFiles: 0,
			expectError:   false,
		},
		{
			name: "Single file in directory",
			setup: func(dir string) error {
				_, err := os.Create(path.Join(dir, "file1.txt"))
				return err
			},
			expectedFile:  "file1.txt",
			expectedFiles: 1,
			expectError:   false,
		},
		{
			name: "Multiple files in directory",
			setup: func(dir string) error {
				for i := 1; i <= 3; i++ {
					if _, err := os.Create(path.Join(dir, fmt.Sprintf("file%d.txt", i))); err != nil {
						return err
					}
				}
				return nil
			},
			expectedFile:  "file1.txt",
			expectedFiles: 3,
			expectError:   false,
		},
		{
			name: "Nested directories",
			setup: func(dir string) error {
				subDir := path.Join(dir, "subdir")
				if err := os.Mkdir(subDir, 0o755); err != nil {
					return err
				}
				if _, err := os.Create(path.Join(subDir, "file1.txt")); err != nil {
					return err
				}
				return nil
			},
			expectedFile:  "subdir/file1.txt",
			expectedFiles: 1,
			expectError:   false,
		},
		{
			name: "Error walking directory",
			setup: func(dir string) error {
				return os.Chmod(dir, 0o000)
			},
			expectedFile:  "",
			expectedFiles: 0,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.setup != nil {
				if err := tt.setup(dir); err != nil {
					t.Errorf("Setup failed: %v", err)
				}
			}
			files, err := getFilesSortedByModTimeAsc(dir)
			if (err != nil) != tt.expectError {
				t.Errorf("getEarliestFile() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				if len(files) != tt.expectedFiles {
					t.Errorf("Expected %d files, got %d", tt.expectedFiles, len(files))
				}
				if tt.expectedFile != "" && !strings.HasSuffix(files[0], tt.expectedFile) {
					t.Errorf("Expected earliest file %s, got %s", tt.expectedFile, files[0])
				}
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		setup       func(path string) error
		expectError bool
		expectedErr string
	}{
		{
			name:        "Valid path",
			path:        t.TempDir(),
			setup:       nil,
			expectError: false,
		},
		{
			name:        "Empty path",
			path:        "",
			setup:       nil,
			expectError: true,
			expectedErr: "path cannot be empty",
		},
		{
			name:        "Path with '..'",
			path:        "../invalid/path",
			setup:       nil,
			expectError: true,
			expectedErr: "path must not contain '..', dir traversal is not allowed",
		},
		{
			name: "Path is a file",
			path: func() string {
				file, err := os.CreateTemp("", "testfile")
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}
				return file.Name()
			}(),
			setup:       nil,
			expectError: true,
			expectedErr: "failed to create directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				if err := tt.setup(tt.path); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}
			err := validatePath(tt.path)
			if (err != nil) != tt.expectError {
				t.Errorf("validatePath() error = %v, expectError %v", err, tt.expectError)
			}
			if tt.expectError && err != nil && !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("Expected error to contain %q, got %q", tt.expectedErr, err.Error())
			}
		})
	}
}

func TestUnmarshalConfig(t *testing.T) {
	tmpPath := t.TempDir()

	tests := []struct {
		name         string
		config       interface{}
		expectedPath string
		expectedMax  float64
		expectedTTL  time.Duration
		expectError  bool
		expectedErr  string
	}{
		{
			name: "Valid config",
			config: map[string]interface{}{
				"path":                tmpPath,
				"maxAuditResults":     3.0,
				"closedConnectionTTL": "1m",
			},
			expectedPath: tmpPath,
			expectedMax:  3.0,
			expectError:  false,
			expectedTTL:  1 * time.Minute,
		},
		{
			name:        "Invalid config format",
			config:      map[int]interface{}{1: "test"},
			expectError: true,
			expectedErr: "invalid config format",
			expectedTTL: 0,
		},
		{
			name: "Missing path",
			config: map[string]interface{}{
				"maxAuditResults": 3.0,
			},
			expectError: true,
			expectedErr: "missing or invalid 'path'",
			expectedTTL: 0,
		},
		{
			name: "Invalid path",
			config: map[string]interface{}{
				"path":            "../invalid/path",
				"maxAuditResults": 3.0,
			},
			expectError: true,
			expectedErr: "invalid path",
			expectedTTL: 0,
		},
		{
			name: "Missing maxAuditResults",
			config: map[string]interface{}{
				"path": tmpPath,
			},
			expectError: true,
			expectedErr: "missing or invalid 'maxAuditResults'",
			expectedTTL: 0,
		},
		{
			name: "Exceeding maxAuditResults",
			config: map[string]interface{}{
				"path":            tmpPath,
				"maxAuditResults": 10.0,
			},
			expectError: true,
			expectedErr: "maxAuditResults cannot be greater than the maximum allowed audit runs",
			expectedTTL: 0,
		},
		{
			name: "Invalid closedConnectionTTL",
			config: map[string]interface{}{
				"path":                tmpPath,
				"maxAuditResults":     3.0,
				"closedConnectionTTL": "invalid",
			},
			expectError:  true,
			expectedErr:  "invalid ttl format: time:",
			expectedTTL:  0,
			expectedMax:  3.0,
			expectedPath: tmpPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, maxResults, ttl, err := unmarshalConfig(tt.config)
			if (err != nil) != tt.expectError {
				t.Errorf("unmarshalConfig() error = %v, expectError %v", err, tt.expectError)
			}
			if tt.expectError && err != nil && !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("Expected error to contain %q, got %q", tt.expectedErr, err.Error())
			}
			if !tt.expectError {
				if path != tt.expectedPath {
					t.Errorf("Expected path %q, got %q", tt.expectedPath, path)
				}
				if maxResults != tt.expectedMax {
					t.Errorf("Expected maxAuditResults %f, got %f", tt.expectedMax, maxResults)
				}
			}
			if ttl != tt.expectedTTL {
				t.Errorf("Expected closedConnectionTTL %v, got %v", tt.expectedTTL, ttl)
			}
		})
	}
}

func TestRetryFailedConnections(t *testing.T) {
	tests := []struct {
		name                   string
		setup                  func(writer *Writer)
		expectedClosedConnsLen int
		validateResult         func(t *testing.T, writer *Writer)
	}{
		{
			name: "Successfully retry and remove connection",
			setup: func(writer *Writer) {
				writer.closedConnections["conn1"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir(), ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    time.Now().Add(-1 * time.Minute),
					RetryCount:  0,
					NextRetryAt: time.Now().Add(-1 * time.Second),
				}
				writer.closeAndRemoveFilesWithRetry = func(_ Connection) error {
					return nil
				}
			},
			expectedClosedConnsLen: 0,
			validateResult: func(t *testing.T, writer *Writer) {
				if !writer.cleanupStopped {
					t.Error("Expected cleanup to be stopped when no connections remain")
				}
			},
		},
		{
			name: "Retry fails, increment retry count",
			setup: func(writer *Writer) {
				writer.closedConnections["conn2"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir(), ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    time.Now().Add(-1 * time.Minute),
					RetryCount:  1,
					NextRetryAt: time.Now().Add(-1 * time.Second),
				}
				writer.closeAndRemoveFilesWithRetry = func(_ Connection) error {
					return fmt.Errorf("forced failure")
				}
			},
			expectedClosedConnsLen: 1,
			validateResult: func(t *testing.T, writer *Writer) {
				if conn, exists := writer.closedConnections["conn2"]; exists {
					if conn.RetryCount != 2 {
						t.Errorf("Expected RetryCount to be 2, got %d", conn.RetryCount)
					}
					if conn.NextRetryAt.Before(time.Now()) {
						t.Error("NextRetryAt should be set to future time after failed retry")
					}
				} else {
					t.Error("Connection should still exist after failed retry")
				}
			},
		},
		{
			name: "Remove connection exceeding max retry attempts",
			setup: func(writer *Writer) {
				writer.closedConnections["conn3"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir(), ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    time.Now().Add(-1 * time.Minute),
					RetryCount:  maxRetryAttempts,
					NextRetryAt: time.Now().Add(-1 * time.Second),
				}
				writer.closeAndRemoveFilesWithRetry = func(_ Connection) error {
					t.Error("closeAndRemoveFilesWithRetry should not be called for max retry connections")
					return nil
				}
			},
			expectedClosedConnsLen: 0,
			validateResult: func(t *testing.T, writer *Writer) {
				if !writer.cleanupStopped {
					t.Error("Expected cleanup to be stopped when no connections remain")
				}
			},
		},
		{
			name: "Remove expired connection",
			setup: func(writer *Writer) {
				writer.closedConnections["conn4"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir(), ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    time.Now().Add(-maxConnectionAge - time.Minute),
					RetryCount:  0,
					NextRetryAt: time.Now().Add(-1 * time.Second),
				}
				writer.closeAndRemoveFilesWithRetry = func(_ Connection) error {
					t.Error("closeAndRemoveFilesWithRetry should not be called for expired connections")
					return nil
				}
			},
			expectedClosedConnsLen: 0,
			validateResult: func(t *testing.T, writer *Writer) {
				if !writer.cleanupStopped {
					t.Error("Expected cleanup to be stopped when no connections remain")
				}
			},
		},
		{
			name: "Skip retry if NextRetryAt is in future",
			setup: func(writer *Writer) {
				writer.closedConnections["conn5"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir(), ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    time.Now().Add(-1 * time.Minute),
					RetryCount:  0,
					NextRetryAt: time.Now().Add(1 * time.Minute),
				}
				writer.closeAndRemoveFilesWithRetry = func(_ Connection) error {
					t.Error("closeAndRemoveFilesWithRetry should not be called when NextRetryAt is in future")
					return nil
				}
			},
			expectedClosedConnsLen: 1,
			validateResult: func(t *testing.T, writer *Writer) {
				if conn, exists := writer.closedConnections["conn5"]; exists {
					if conn.RetryCount != 0 {
						t.Errorf("Expected RetryCount to remain 0, got %d", conn.RetryCount)
					}
				} else {
					t.Error("Connection should still exist when NextRetryAt is in future")
				}
				if writer.cleanupStopped {
					t.Error("Expected cleanup to remain running when connections still exist")
				}
			},
		},
		{
			name: "Multiple connections with mixed outcomes",
			setup: func(writer *Writer) {
				now := time.Now()
				writer.closedConnections["success"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir() + "/success", ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    now.Add(-1 * time.Minute),
					RetryCount:  0,
					NextRetryAt: now.Add(-1 * time.Second),
				}
				writer.closedConnections["fail"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir(), ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    now.Add(-1 * time.Minute),
					RetryCount:  0,
					NextRetryAt: now.Add(-1 * time.Second),
				}
				writer.closedConnections["not-ready"] = FailedConnection{
					Connection:  Connection{Path: t.TempDir(), ClosedConnectionTTL: maxConnectionAge},
					FailedAt:    now.Add(-1 * time.Minute),
					RetryCount:  0,
					NextRetryAt: now.Add(1 * time.Minute),
				}

				writer.closeAndRemoveFilesWithRetry = func(conn Connection) error {
					if strings.Contains(conn.Path, "success") {
						return nil
					}
					return fmt.Errorf("simulated failure")
				}
			},
			expectedClosedConnsLen: 2,
			validateResult: func(t *testing.T, writer *Writer) {
				if _, exists := writer.closedConnections["success"]; exists {
					t.Error("Success connection should have been removed")
				}
				if _, exists := writer.closedConnections["fail"]; !exists {
					t.Error("Failed connection should still exist")
				}
				if _, exists := writer.closedConnections["not-ready"]; !exists {
					t.Error("Not-ready connection should still exist")
				}

				if writer.cleanupStopped {
					t.Error("Expected cleanup to remain running when connections still exist")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &Writer{
				mu:                sync.RWMutex{},
				openConnections:   make(map[string]Connection),
				closedConnections: make(map[string]FailedConnection),
				cleanupDone:       make(chan struct{}),
				cleanupStopped:    false,
				closeAndRemoveFilesWithRetry: func(_ Connection) error {
					return nil
				},
			}

			tt.setup(writer)

			writer.retryFailedConnections()

			writer.mu.RLock()
			actualLen := len(writer.closedConnections)
			writer.mu.RUnlock()

			if actualLen != tt.expectedClosedConnsLen {
				t.Errorf("expected %d closed connections, got %d", tt.expectedClosedConnsLen, actualLen)
			}

			if tt.validateResult != nil {
				writer.mu.RLock()
				writerCopy := &Writer{
					closedConnections: make(map[string]FailedConnection),
					cleanupStopped:    writer.cleanupStopped,
				}
				for k, v := range writer.closedConnections {
					writerCopy.closedConnections[k] = v
				}
				writer.mu.RUnlock()

				tt.validateResult(t, writerCopy)
			}

			writer.mu.RLock()
			cleanupStopped := writer.cleanupStopped
			writer.mu.RUnlock()

			if cleanupStopped {
				select {
				case <-writer.cleanupDone:
				case <-time.After(100 * time.Millisecond):
					t.Error("Expected cleanupDone channel to be closed when cleanup is stopped")
				}
			}
		})
	}
}

func TestRetryFailedConnectionsConcurrent(t *testing.T) {
	t.Run("concurrent retries keep state consistent", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				// Always fail so that connections stay in the retry queue and
				// we exercise the backoff and bookkeeping logic under
				// concurrent access.
				return fmt.Errorf("forced failure")
			},
		}

		now := time.Now()

		// Seed multiple failed connections that are all eligible for retry.
		const failedConnections = 5
		for i := 0; i < failedConnections; i++ {
			name := fmt.Sprintf("conn-concurrent-%d", i)
			writer.closedConnections[name] = FailedConnection{
				Connection: Connection{
					Path:                t.TempDir(),
					MaxAuditResults:     5,
					ClosedConnectionTTL: maxConnectionAge,
				},
				FailedAt:    now.Add(-1 * time.Minute),
				RetryCount:  0,
				NextRetryAt: now.Add(-1 * time.Second),
			}
		}

		var wg sync.WaitGroup
		const workers = 8

		// Invoke retryFailedConnections from multiple goroutines to simulate
		// the background cleanup loop racing with other callers in a
		// multi-threaded environment. The Writer's internal locking should
		// ensure a consistent view of the retry state without panics.
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				writer.retryFailedConnections()
			}()
		}

		wg.Wait()

		writer.mu.RLock()
		defer writer.mu.RUnlock()

		if len(writer.closedConnections) == 0 {
			t.Fatalf("expected failed connections to remain after retries, got 0")
		}

		for name, fc := range writer.closedConnections {
			if fc.RetryCount == 0 {
				t.Errorf("expected RetryCount for %s to be incremented, got 0", name)
			}
			if !fc.NextRetryAt.After(now) {
				t.Errorf("expected NextRetryAt for %s to be in the future, got %v", name, fc.NextRetryAt)
			}
		}
	})
}

func TestBackgroundCleanupLifecycle(t *testing.T) {
	t.Run("Background cleanup starts on first CreateConnection", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			cleanupOnce:       sync.Once{},
			cleanupStopped:    false,
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return nil
			},
		}

		ctx := context.Background()
		tmpDir := t.TempDir()
		config := map[string]interface{}{
			"path":            tmpDir,
			"maxAuditResults": 5.0,
		}

		err := writer.CreateConnection(ctx, "test-conn", config)
		if err != nil {
			t.Fatalf("CreateConnection failed: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		writer.mu.RLock()
		_, exists := writer.openConnections["test-conn"]
		cleanupStopped := writer.cleanupStopped
		writer.mu.RUnlock()

		if !exists {
			t.Error("Connection was not created")
		}

		if cleanupStopped {
			t.Error("Expected cleanup to be running, but it's stopped")
		}

		writer.mu.Lock()
		if !writer.cleanupStopped {
			writer.cleanupStopped = true
			close(writer.cleanupDone)
		}
		writer.mu.Unlock()
	})

	t.Run("Background cleanup stops when all connections are closed", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			cleanupOnce:       sync.Once{},
			cleanupStopped:    false,
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return nil
			},
		}

		ctx := context.Background()
		tmpDir := t.TempDir()
		config := map[string]interface{}{
			"path":            tmpDir,
			"maxAuditResults": 5.0,
		}

		err := writer.CreateConnection(ctx, "test-conn", config)
		if err != nil {
			t.Fatalf("CreateConnection failed: %v", err)
		}

		err = writer.CloseConnection("test-conn")
		if err != nil {
			t.Fatalf("CloseConnection failed: %v", err)
		}

		writer.retryFailedConnections()

		writer.mu.RLock()
		cleanupStopped := writer.cleanupStopped
		writer.mu.RUnlock()

		if !cleanupStopped {
			t.Error("Expected cleanup to be stopped after all connections closed")
		}

		select {
		case <-writer.cleanupDone:
		case <-time.After(1 * time.Second):
			t.Error("Expected cleanupDone channel to be closed within timeout")
		}
	})

	t.Run("Background cleanup restarts after being stopped", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			cleanupOnce:       sync.Once{},
			cleanupStopped:    false,
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return nil
			},
		}

		ctx := context.Background()
		tmpDir := t.TempDir()
		config := map[string]interface{}{
			"path":            tmpDir,
			"maxAuditResults": 5.0,
		}

		err := writer.CreateConnection(ctx, "test-conn-1", config)
		if err != nil {
			t.Fatalf("CreateConnection failed: %v", err)
		}

		err = writer.CloseConnection("test-conn-1")
		if err != nil {
			t.Fatalf("CloseConnection failed: %v", err)
		}

		writer.retryFailedConnections()

		writer.mu.RLock()
		cleanupStopped := writer.cleanupStopped
		writer.mu.RUnlock()

		if !cleanupStopped {
			t.Error("Expected cleanup to be stopped after first phase")
		}

		writer.closeAndRemoveFilesWithRetry = func(_ Connection) error {
			return fmt.Errorf("simulated failure")
		}

		err = writer.CreateConnection(ctx, "test-conn-2", config)
		if err != nil {
			t.Fatalf("Second CreateConnection failed: %v", err)
		}

		_ = writer.CloseConnection("test-conn-2")

		writer.mu.RLock()
		cleanupStoppedAfterRestart := writer.cleanupStopped
		writer.mu.RUnlock()

		if cleanupStoppedAfterRestart {
			t.Error("Expected cleanup to be restarted, but it's still stopped")
		}

		select {
		case <-writer.cleanupDone:
			t.Error("New cleanupDone channel should not be closed yet")
		case <-time.After(100 * time.Millisecond):
		}

		if !closedConnectionExists(writer, "test-conn-2") {
			t.Error("Second connection should be in closedConnections due to simulated failure")
		}

		writer.mu.Lock()
		if !writer.cleanupStopped {
			writer.cleanupStopped = true
			close(writer.cleanupDone)
		}
		writer.mu.Unlock()
	})
}

func TestCloseConnectionWithFailedRetries(t *testing.T) {
	t.Run("Failed close adds connection to closedConnections", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return fmt.Errorf("simulated failure")
			},
		}

		tmpDir := t.TempDir()
		writer.openConnections["failing-conn"] = Connection{
			Path:            tmpDir,
			MaxAuditResults: 5,
		}

		err := writer.CloseConnection("failing-conn")
		if err == nil {
			t.Error("Expected CloseConnection to return an error")
		}

		writer.mu.RLock()
		_, existsInOpen := writer.openConnections["failing-conn"]
		writer.mu.RUnlock()

		if !closedConnectionExists(writer, "failing-conn") {
			t.Error("Expected connection to be in closedConnections")
		}

		if existsInOpen {
			t.Error("Connection should have been removed from openConnections")
		}

		failedConns := getClosedConnections(writer, "failing-conn")

		for _, conn := range failedConns {
			if conn.RetryCount != 0 {
				t.Errorf("Expected initial RetryCount to be 0, got %d", conn.RetryCount)
			}

			if conn.FailedAt.IsZero() {
				t.Error("Expected FailedAt to be set")
			}

			if conn.NextRetryAt.IsZero() {
				t.Error("Expected NextRetryAt to be set")
			}
		}
	})

	t.Run("Background cleanup retries failed connections", func(t *testing.T) {
		callCount := 0
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				callCount++
				if callCount <= 2 {
					return fmt.Errorf("retry %d failed", callCount)
				}
				return nil
			},
		}

		tmpDir := t.TempDir()
		now := time.Now()

		writer.closedConnections["retry-conn"] = FailedConnection{
			Connection: Connection{
				Path:                tmpDir,
				MaxAuditResults:     5,
				ClosedConnectionTTL: 2 * time.Minute,
			},
			FailedAt:    now.Add(-1 * time.Minute),
			RetryCount:  0,
			NextRetryAt: now.Add(-30 * time.Second),
		}

		writer.retryFailedConnections()

		if callCount != 1 {
			t.Errorf("Expected 1 call to closeAndRemoveFilesWithRetry, got %d", callCount)
		}

		writer.mu.RLock()
		_, exists := writer.closedConnections["retry-conn"]
		writer.mu.RUnlock()

		if !exists {
			t.Error("Connection should still be in closedConnections after failed retry")
		}

		writer.mu.RLock()
		failedConn := writer.closedConnections["retry-conn"]
		writer.mu.RUnlock()

		if failedConn.RetryCount != 1 {
			t.Errorf("Expected RetryCount to be 1, got %d", failedConn.RetryCount)
		}

		failedConn.NextRetryAt = now.Add(-1 * time.Second)
		writer.mu.Lock()
		writer.closedConnections["retry-conn"] = failedConn
		writer.mu.Unlock()

		writer.retryFailedConnections()

		if callCount != 2 {
			t.Errorf("Expected 2 calls to closeAndRemoveFilesWithRetry, got %d", callCount)
		}

		writer.mu.RLock()
		failedConn = writer.closedConnections["retry-conn"]
		writer.mu.RUnlock()

		failedConn.NextRetryAt = now.Add(-1 * time.Second)
		writer.mu.Lock()
		writer.closedConnections["retry-conn"] = failedConn
		writer.mu.Unlock()

		writer.retryFailedConnections()

		if callCount != 3 {
			t.Errorf("Expected 3 calls to closeAndRemoveFilesWithRetry, got %d", callCount)
		}

		writer.mu.RLock()
		_, exists = writer.closedConnections["retry-conn"]
		writer.mu.RUnlock()

		if exists {
			t.Error("Connection should have been removed from closedConnections after successful retry")
		}
	})

	t.Run("Connections are removed after max retry attempts", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return fmt.Errorf("always fails")
			},
		}

		tmpDir := t.TempDir()
		now := time.Now()

		writer.closedConnections["max-retries-conn"] = FailedConnection{
			Connection: Connection{
				Path:            tmpDir,
				MaxAuditResults: 5,
			},
			FailedAt:    now.Add(-10 * time.Minute),
			RetryCount:  maxRetryAttempts,
			NextRetryAt: now.Add(-1 * time.Second),
		}

		writer.retryFailedConnections()

		writer.mu.RLock()
		_, exists := writer.closedConnections["max-retries-conn"]
		writer.mu.RUnlock()

		if exists {
			t.Error("Connection should have been removed after exceeding max retry attempts")
		}
	})

	t.Run("Expired connections are removed", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return fmt.Errorf("not called")
			},
		}

		tmpDir := t.TempDir()
		now := time.Now()

		writer.closedConnections["expired-conn"] = FailedConnection{
			Connection: Connection{
				Path:            tmpDir,
				MaxAuditResults: 5,
			},
			FailedAt:    now.Add(-maxConnectionAge - time.Minute),
			RetryCount:  2,
			NextRetryAt: now.Add(-1 * time.Second),
		}

		writer.retryFailedConnections()

		writer.mu.RLock()
		_, exists := writer.closedConnections["expired-conn"]
		writer.mu.RUnlock()

		if exists {
			t.Error("Expired connection should have been removed")
		}
	})
}

func closedConnectionExists(writer *Writer, connName string) bool {
	writer.mu.RLock()
	defer writer.mu.RUnlock()
	for name := range writer.closedConnections {
		if strings.Contains(name, connName) {
			return true
		}
	}
	return false
}

func getClosedConnections(writer *Writer, connName string) []FailedConnection {
	writer.mu.RLock()
	defer writer.mu.RUnlock()
	var conns []FailedConnection
	for name := range writer.closedConnections {
		if strings.Contains(name, connName) {
			conns = append(conns, writer.closedConnections[name])
		}
	}
	return conns
}

func TestConnectionTTL(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]interface{}
		expectedTTL time.Duration
		expectError bool
	}{
		{
			name: "Valid TTL string",
			config: map[string]interface{}{
				"path":                t.TempDir(),
				"maxAuditResults":     5.0,
				"closedConnectionTTL": "5m",
			},
			expectedTTL: 5 * time.Minute,
			expectError: false,
		},
		{
			name: "Invalid TTL string",
			config: map[string]interface{}{
				"path":                t.TempDir(),
				"maxAuditResults":     5.0,
				"closedConnectionTTL": "invalid",
			},
			expectedTTL: 0,
			expectError: true,
		},
		{
			name: "No TTL specified",
			config: map[string]interface{}{
				"path":            t.TempDir(),
				"maxAuditResults": 5.0,
			},
			expectedTTL: maxConnectionAge,
			expectError: false,
		},
		{
			name: "TTL as non-string",
			config: map[string]interface{}{
				"path":                t.TempDir(),
				"maxAuditResults":     5.0,
				"closedConnectionTTL": 123,
			},
			expectedTTL: maxConnectionAge,
			expectError: false,
		},
		{
			name: "Complex TTL format",
			config: map[string]interface{}{
				"path":                t.TempDir(),
				"maxAuditResults":     5.0,
				"closedConnectionTTL": "2m5s",
			},
			expectedTTL: 2*time.Minute + 5*time.Second,
			expectError: false,
		},
		{
			name: "TTL too long",
			config: map[string]interface{}{
				"path":                t.TempDir(),
				"maxAuditResults":     5.0,
				"closedConnectionTTL": "2h5m",
			},
			expectedTTL: 0,
			expectError: true,
		},
		{
			name: "TTL too short",
			config: map[string]interface{}{
				"path":                t.TempDir(),
				"maxAuditResults":     5.0,
				"closedConnectionTTL": "1s",
			},
			expectedTTL: 0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &Writer{
				mu:                sync.RWMutex{},
				openConnections:   make(map[string]Connection),
				closedConnections: make(map[string]FailedConnection),
				cleanupDone:       make(chan struct{}),
				closeAndRemoveFilesWithRetry: func(_ Connection) error {
					return nil
				},
			}

			err := writer.CreateConnection(context.Background(), "test-conn", tt.config)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError {
				writer.mu.RLock()
				conn, exists := writer.openConnections["test-conn"]
				writer.mu.RUnlock()

				if !exists {
					t.Fatal("Connection was not created")
				}

				if conn.ClosedConnectionTTL != tt.expectedTTL {
					t.Errorf("Expected TTL %v, got %v", tt.expectedTTL, conn.ClosedConnectionTTL)
				}
			}
		})
	}
}

func TestTTLBasedConnectionRemoval(t *testing.T) {
	t.Run("Connection removed after custom TTL expires", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return fmt.Errorf("always fail")
			},
		}

		config := map[string]interface{}{
			"path":            t.TempDir(),
			"maxAuditResults": 5.0,
		}

		err := writer.CreateConnection(context.Background(), "short-ttl-conn", config)
		if err != nil {
			t.Fatalf("Failed to create connection: %v", err)
		}

		writer.mu.RLock()
		conn := writer.openConnections["short-ttl-conn"]
		writer.mu.RUnlock()

		conn.ClosedConnectionTTL = 100 * time.Millisecond

		if conn.ClosedConnectionTTL != 100*time.Millisecond {
			t.Errorf("Expected TTL 100ms, got %v", conn.ClosedConnectionTTL)
		}

		err = writer.CloseConnection("short-ttl-conn")
		if err == nil {
			t.Error("Expected CloseConnection to fail")
		}

		if !closedConnectionExists(writer, "short-ttl-conn") {
			t.Fatal("Connection should be in closedConnections")
		}

		time.Sleep(150 * time.Millisecond)

		writer.retryFailedConnections()

		writer.mu.RLock()
		_, stillExists := writer.closedConnections["short-ttl-conn"]
		writer.mu.RUnlock()

		if stillExists {
			t.Error("Connection should have been removed after TTL expiration")
		}
	})

	t.Run("Connection with longer TTL remains in queue", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return fmt.Errorf("always fail")
			},
		}

		config := map[string]interface{}{
			"path":                t.TempDir(),
			"maxAuditResults":     5.0,
			"closedConnectionTTL": "1m",
		}

		err := writer.CreateConnection(context.Background(), "long-ttl-conn", config)
		if err != nil {
			t.Fatalf("Failed to create connection: %v", err)
		}
		writer.mu.RLock()
		conn := writer.openConnections["long-ttl-conn"]
		conn.ClosedConnectionTTL = 10 * time.Second
		writer.openConnections["long-ttl-conn"] = conn
		writer.mu.RUnlock()

		err = writer.CloseConnection("long-ttl-conn")
		if err == nil {
			t.Error("Expected CloseConnection to fail")
		}

		time.Sleep(100 * time.Millisecond)

		writer.retryFailedConnections()

		if !closedConnectionExists(writer, "long-ttl-conn") {
			t.Error("Connection should still exist (TTL not expired)")
		}
	})

	t.Run("Multiple connections with different TTLs", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.RWMutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ Connection) error {
				return fmt.Errorf("always fail")
			},
		}

		shortConfig := map[string]interface{}{
			"path":            t.TempDir(),
			"maxAuditResults": 5.0,
		}
		err := writer.CreateConnection(context.Background(), "short-conn", shortConfig)
		if err != nil {
			t.Fatalf("Failed to create short connection: %v", err)
		}
		longConfig := map[string]interface{}{
			"path":            t.TempDir(),
			"maxAuditResults": 5.0,
		}
		err = writer.CreateConnection(context.Background(), "long-conn", longConfig)
		if err != nil {
			t.Fatalf("Failed to create long connection: %v", err)
		}
		writer.mu.Lock()
		conn := writer.openConnections["short-conn"]
		conn.ClosedConnectionTTL = 100 * time.Millisecond
		writer.openConnections["short-conn"] = conn
		conn = writer.openConnections["long-conn"]
		conn.ClosedConnectionTTL = 10 * time.Second
		writer.openConnections["long-conn"] = conn
		writer.mu.Unlock()

		_ = writer.CloseConnection("short-conn")
		_ = writer.CloseConnection("long-conn")

		writer.mu.RLock()
		shortCount := len(writer.closedConnections)
		writer.mu.RUnlock()

		if shortCount != 2 {
			t.Errorf("Expected 2 connections in closedConnections, got %d", shortCount)
		}

		time.Sleep(100 * time.Millisecond)

		writer.retryFailedConnections()
		writer.mu.RLock()
		finalCount := len(writer.closedConnections)
		writer.mu.RUnlock()

		if closedConnectionExists(writer, "short-conn") {
			t.Error("Short TTL connection should have been removed")
		}
		if !closedConnectionExists(writer, "long-conn") {
			t.Error("Long TTL connection should still exist")
		}
		if finalCount != 1 {
			t.Errorf("Expected 1 connection remaining, got %d", finalCount)
		}
	})
}
