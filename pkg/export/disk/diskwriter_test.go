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

	tests := []struct {
		name           string
		connectionName string
		config         interface{}
		expectError    bool
	}{
		{
			name:           "Valid config",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":            "/tmp/audit",
				"maxAuditResults": 3.0,
			},
			expectError: false,
		},
		{
			name:           "Invalid config format",
			connectionName: "conn2",
			config:         "invalid config",
			expectError:    true,
		},
		{
			name:           "Missing path",
			connectionName: "conn3",
			config: map[string]interface{}{
				"maxAuditResults": 10.0,
			},
			expectError: true,
		},
		{
			name:           "Missing maxAuditResults",
			connectionName: "conn4",
			config: map[string]interface{}{
				"path":      "/tmp/audit",
			},
			expectError: true,
		},
		{
			name:           "Exceeding maxAuditResults",
			connectionName: "conn4",
			config: map[string]interface{}{
				"path":      "/tmp/audit",
				"maxAuditResults": 10.0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writer.CreateConnection(context.Background(), tt.connectionName, tt.config)
			if (err != nil) != tt.expectError {
				t.Errorf("CreateConnection() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				conn, exists := writer.openConnections[tt.connectionName]
				if !exists {
					t.Errorf("Connection %s was not created", tt.connectionName)
				}
				path, pathOk := tt.config.(map[string]interface{})["path"].(string)
				if !pathOk {
					t.Fatalf("Failed to get path from config")
				}
				if conn.Path != path {
					t.Errorf("Expected path %s, got %s", path, conn.Path)
				}
				maxAuditResults, maxResultsOk := tt.config.(map[string]interface{})["maxAuditResults"].(float64)
				if !maxResultsOk {
					t.Fatalf("Failed to get maxAuditResults from config")
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

	// Pre-create a connection to update
	writer.openConnections["conn1"] = Connection{
		Path:            "/tmp/audit",
		MaxAuditResults: 3,
	}

	tests := []struct {
		name           string
		connectionName string
		config         interface{}
		expectError    bool
	}{
		{
			name:           "Valid update",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":            "/tmp/audit_updated",
				"maxAuditResults": 4.0,
			},
			expectError: false,
		},
		{
			name:           "Invalid config format",
			connectionName: "conn1",
			config:         "invalid config",
			expectError:    true,
		},
		{
			name:           "Connection not found",
			connectionName: "conn2",
			config: map[string]interface{}{
				"path":            "/tmp/audit",
				"maxAuditResults": 2.0,
			},
			expectError: true,
		},
		{
			name:           "Missing path",
			connectionName: "conn1",
			config: map[string]interface{}{
				"maxAuditResults": 2.0,
			},
			expectError: true,
		},
		{
			name:           "Missing maxAuditResults",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":      "/tmp/audit",
			},
			expectError: true,
		},
		{
			name:           "Exceeding maxAuditResults",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":      "/tmp/audit",
				"maxAuditResults": 10.0,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writer.UpdateConnection(context.Background(), tt.connectionName, tt.config)
			if (err != nil) != tt.expectError {
				t.Errorf("UpdateConnection() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				conn, exists := writer.openConnections[tt.connectionName]
				if !exists {
					t.Errorf("Connection %s was not found", tt.connectionName)
				}
				path, pathOk := tt.config.(map[string]interface{})["path"].(string)
				if !pathOk {
					t.Fatalf("Failed to get path from config")
				}
				if conn.Path != path {
					t.Errorf("Expected path %s, got %s", path, conn.Path)
				}
				maxAuditResults, maxResultsOk := tt.config.(map[string]interface{})["maxAuditResults"].(float64)
				if !maxResultsOk {
					t.Fatalf("Failed to get maxAuditResults from config")
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

	// Pre-create a connection to close
	writer.openConnections["conn1"] = Connection{
		Path:            "/tmp/audit",
		MaxAuditResults: 10,
	}

	tests := []struct {
		name           string
		connectionName string
		expectError    bool
	}{
		{
			name:           "Valid close",
			connectionName: "conn1",
			expectError:    false,
		},
		{
			name:           "Connection not found",
			connectionName: "conn2",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
		Path:            "/tmp/audit",
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
					t.Fatalf("Failed to list files: %v", err)
				}
				msg, ok := tt.data.(util.ExportMsg)
				if !ok {
					t.Fatalf("Failed to convert data to ExportMsg")
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
						t.Fatalf("Failed to read file: %v", err)
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

	err := os.RemoveAll("/tmp/audit")
	if err != nil {
		t.Fatalf("Failed to clean up: %v", err)
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
				Path: "/tmp/audit",
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

	err := os.RemoveAll("/tmp/audit")
	if err != nil {
		t.Fatalf("Failed to clean up: %v", err)
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
				Path:            "/tmp/audit",
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
				Path:            "/tmp/audit",
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
					t.Fatalf("Setup failed: %v", err)
				}
			}
			err := tt.connection.handleAuditEnd(tt.topic)
			if (err != nil) != tt.expectError {
				t.Errorf("handleAuditEnd() error = %v, expectError %v", err, tt.expectError)
			}

			if !tt.expectError {
				files, err := listFiles(path.Join(tt.connection.Path, tt.topic))
				if err != nil {
					t.Fatalf("Failed to list files: %v", err)
				}
				if slices.Contains(files, tt.expectedFile) {
					t.Errorf("Expected file %s to exist, but it does not. Files: %v", tt.expectedFile, files)
				}
			}
		})
	}

	err := os.RemoveAll("/tmp/audit")
	if err != nil {
		t.Fatalf("Failed to clean up: %v", err)
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
				Path: "/tmp/audit",
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
				Path: "/tmp/audit",
			},
			setup:       nil,
			expectError: true,
		},
		{
			name: "Invalid file descriptor",
			connection: Connection{
				Path: "/tmp/audit",
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
					t.Fatalf("Setup failed: %v", err)
				}
			}
			err := tt.connection.unlockAndCloseFile()
			if (err != nil) != tt.expectError {
				t.Errorf("unlockAndCloseFile() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}

	err := os.RemoveAll("/tmp/audit")
	if err != nil {
		t.Fatalf("Failed to clean up: %v", err)
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
				Path:            "/tmp/audit",
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
				Path:            "/tmp/audit",
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
				Path:            "/tmp/audit",
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
				Path:            "/invalid/path",
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
					t.Fatalf("Setup failed: %v", err)
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
					t.Fatalf("Failed to read directory: %v", err)
				}
				if len(files) != tt.expectedFiles {
					t.Errorf("Expected %d files, got %d", tt.expectedFiles, len(files))
				}
			}
		})
	}
	err := os.RemoveAll("/tmp/audit")
	if err != nil {
		t.Fatalf("Failed to clean up: %v", err)
	}
}

func TestGetEarliestFile(t *testing.T) {
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
					t.Fatalf("Setup failed: %v", err)
				}
			}
			earliestFile, files, err := getEarliestFile(dir)
			if (err != nil) != tt.expectError {
				t.Errorf("getEarliestFile() error = %v, expectError %v", err, tt.expectError)
			}
			if !tt.expectError {
				if len(files) != tt.expectedFiles {
					t.Errorf("Expected %d files, got %d", tt.expectedFiles, len(files))
				}
				if tt.expectedFile != "" && !strings.HasSuffix(earliestFile, tt.expectedFile) {
					t.Errorf("Expected earliest file %s, got %s", tt.expectedFile, earliestFile)
				}
			}
		})
	}
}
