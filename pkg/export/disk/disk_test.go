package disk

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
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
			err:         fmt.Errorf("error creating connection conn2: invalid config format"),
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
		openConnections: make(map[string]Connection),
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
			err:         fmt.Errorf("error updating connection conn1: invalid config format"),
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
	// Add to check clean up
	writer := &Writer{
		openConnections: make(map[string]Connection),
	}

	tests := []struct {
		name           string
		connectionName string
		setup          func() error
		expectError    bool
	}{
		{
			name:           "Valid close",
			connectionName: "conn1",
			setup: func() error {
				// Pre-create a connection to close
				writer.openConnections["conn1"] = Connection{
					Path:            t.TempDir(),
					MaxAuditResults: 10,
				}
				return nil
			},
			expectError: false,
		},
		{
			name:           "Connection not found",
			connectionName: "conn2",
			setup:          nil,
			expectError:    true,
		},
		{
			name:           "Valid close with open and locked file",
			connectionName: "conn3",
			setup: func() error {
				// Pre-create a connection to close
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
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				if err := tt.setup(); err != nil {
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
		})
	}
}

func TestPublish(t *testing.T) {
	writer := &Writer{
		openConnections: make(map[string]Connection),
	}

	// Pre-create a connection to publish to
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
				// Create an extra file to trigger cleanup
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
				file.Close() // Close the file to make the descriptor invalid
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
		expectError  bool
		expectedErr  string
	}{
		{
			name: "Valid config",
			config: map[string]interface{}{
				"path":            tmpPath,
				"maxAuditResults": 3.0,
			},
			expectedPath: tmpPath,
			expectedMax:  3.0,
			expectError:  false,
		},
		{
			name:        "Invalid config format",
			config:      map[int]interface{}{1: "test"},
			expectError: true,
			expectedErr: "invalid config format",
		},
		{
			name: "Missing path",
			config: map[string]interface{}{
				"maxAuditResults": 3.0,
			},
			expectError: true,
			expectedErr: "missing or invalid 'path'",
		},
		{
			name: "Invalid path",
			config: map[string]interface{}{
				"path":            "../invalid/path",
				"maxAuditResults": 3.0,
			},
			expectError: true,
			expectedErr: "invalid path",
		},
		{
			name: "Missing maxAuditResults",
			config: map[string]interface{}{
				"path": tmpPath,
			},
			expectError: true,
			expectedErr: "missing or invalid 'maxAuditResults'",
		},
		{
			name: "Exceeding maxAuditResults",
			config: map[string]interface{}{
				"path":            tmpPath,
				"maxAuditResults": 10.0,
			},
			expectError: true,
			expectedErr: "maxAuditResults cannot be greater than the maximum allowed audit runs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, maxResults, err := unmarshalConfig(tt.config)
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
		})
	}
}
