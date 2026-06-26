package disk

import (
	"context"
	"fmt"
	"os"
	"path"
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
		{
			name:           "Negative maxAuditResults",
			connectionName: "conn5",
			config: map[string]interface{}{
				"path":            tmpPath,
				"maxAuditResults": -1.0,
			},
			err:         fmt.Errorf("error creating connection conn5: maxAuditResults cannot be negative"),
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
		closeAndRemoveFilesWithRetry: func(conn *Connection) error {
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
		{
			name:           "Negative maxAuditResults",
			connectionName: "conn1",
			config: map[string]interface{}{
				"path":            t.TempDir(),
				"maxAuditResults": -1.0,
			},
			expectError: true,
			err:         fmt.Errorf("error updating connection conn1: maxAuditResults cannot be negative"),
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

func TestUpdateConnectionKeepsConnectionVisibleDuringPathCleanup(t *testing.T) {
	oldPath := t.TempDir()
	newPath := t.TempDir()
	cleanupStarted := make(chan struct{})
	allowCleanup := make(chan struct{})
	var cleanupStartedOnce sync.Once
	var allowCleanupOnce sync.Once
	releaseCleanup := func() {
		allowCleanupOnce.Do(func() {
			close(allowCleanup)
		})
	}
	defer releaseCleanup()

	writer := newTestWriter(func(conn *Connection) error {
		if conn.Path == oldPath {
			cleanupStartedOnce.Do(func() {
				close(cleanupStarted)
			})
			<-allowCleanup
		}
		return nil
	})

	const connectionName = "path-update-conn"
	oldConfig := diskConfig(oldPath, 5.0)
	newConfig := diskConfig(newPath, 4.0)

	requireCreateConnection(t, writer, connectionName, oldConfig)

	updateErr := make(chan error, 1)
	go func() {
		updateErr <- writer.UpdateConnection(context.Background(), connectionName, newConfig)
	}()

	select {
	case <-cleanupStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for path cleanup to start")
	}

	if !openConnectionExists(writer, connectionName) {
		t.Fatalf("connection %s disappeared while path cleanup was in progress", connectionName)
	}

	releaseCleanup()

	select {
	case err := <-updateErr:
		if err != nil {
			t.Fatalf("UpdateConnection() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for UpdateConnection to finish")
	}

	writer.mu.Lock()
	conn := writer.openConnections[connectionName]
	writer.mu.Unlock()
	if conn.Path != newPath {
		t.Fatalf("expected path %s, got %s", newPath, conn.Path)
	}
}

func TestUpdateConnectionPersistsClosedFileOnCleanupError(t *testing.T) {
	oldPath := t.TempDir()
	newPath := t.TempDir()
	file, err := os.CreateTemp(oldPath, "audit")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("Flock() error = %v", err)
	}

	writer := newTestWriter(func(conn *Connection) error {
		if conn.File != nil {
			if err := conn.unlockAndCloseFile(); err != nil {
				return err
			}
			conn.File = nil
		}
		return fmt.Errorf("forced remove failure")
	})
	writer.openConnections["conn1"] = Connection{
		Path:            oldPath,
		MaxAuditResults: 3,
		File:            file,
	}

	err = writer.UpdateConnection(context.Background(), "conn1", diskConfig(newPath, 3.0))
	if err == nil || !strings.Contains(err.Error(), "forced remove failure") {
		t.Fatalf("expected cleanup failure, got %v", err)
	}

	writer.mu.Lock()
	conn := writer.openConnections["conn1"]
	writer.mu.Unlock()
	if conn.File != nil {
		t.Fatal("expected closed file state to be persisted")
	}
	if conn.Path != oldPath {
		t.Fatalf("expected path to remain %s, got %s", oldPath, conn.Path)
	}
}

func TestUpdateConnectionDoesNotRemoveSharedOldPath(t *testing.T) {
	writer := &Writer{
		openConnections:              make(map[string]Connection),
		closedConnections:            make(map[string]FailedConnection),
		cleanupDone:                  make(chan struct{}),
		closeAndRemoveFilesWithRetry: closeAndRemoveFilesWithRetry,
	}

	sharedPath := t.TempDir()
	newPath := t.TempDir()
	remainingFile := path.Join(sharedPath, "audit", "remaining.log")
	if err := os.MkdirAll(path.Dir(remainingFile), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(remainingFile, []byte("remaining"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	writer.openConnections["conn1"] = Connection{Path: sharedPath}
	writer.openConnections["conn2"] = Connection{Path: sharedPath}

	if err := writer.UpdateConnection(context.Background(), "conn1", diskConfig(newPath, 5.0)); err != nil {
		t.Fatalf("UpdateConnection() error = %v", err)
	}

	if _, err := os.Stat(remainingFile); err != nil {
		t.Fatalf("expected shared old-path file to remain for active connection: %v", err)
	}
	writer.mu.Lock()
	conn := writer.openConnections["conn1"]
	writer.mu.Unlock()
	if conn.Path != newPath {
		t.Fatalf("expected conn1 path %s, got %s", newPath, conn.Path)
	}
}

func TestCloseConnection(t *testing.T) {
	writer := &Writer{
		openConnections:   make(map[string]Connection),
		closedConnections: make(map[string]FailedConnection),
		cleanupDone:       make(chan struct{}),
		closeAndRemoveFilesWithRetry: func(conn *Connection) error {
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
				writer.closeAndRemoveFilesWithRetry = func(_ *Connection) error {
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

func TestCloseConnectionDoesNotRemoveSharedPath(t *testing.T) {
	writer := &Writer{
		openConnections:              make(map[string]Connection),
		closedConnections:            make(map[string]FailedConnection),
		cleanupDone:                  make(chan struct{}),
		closeAndRemoveFilesWithRetry: closeAndRemoveFilesWithRetry,
	}

	sharedPath := t.TempDir()
	remainingFile := path.Join(sharedPath, "audit", "remaining.log")
	if err := os.MkdirAll(path.Dir(remainingFile), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(remainingFile, []byte("remaining"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	writer.openConnections["conn1"] = Connection{Path: sharedPath}
	writer.openConnections["conn2"] = Connection{Path: sharedPath}

	if err := writer.CloseConnection("conn1"); err != nil {
		t.Fatalf("CloseConnection() error = %v", err)
	}

	if _, err := os.Stat(remainingFile); err != nil {
		t.Fatalf("expected shared-path file to remain for active connection: %v", err)
	}
	if !openConnectionExists(writer, "conn2") {
		t.Fatal("expected conn2 to remain open")
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
					expectedFile := writer.openConnections[tt.connectionName].currentAuditRun + ".txt"
					if !slices.ContainsFunc(files, func(file string) bool {
						return path.Base(file) == expectedFile
					}) {
						t.Errorf("Expected file %s to exist, but it does not", writer.openConnections[tt.connectionName].currentAuditRun+".txt")
					}
				}
				if msg.Message == "audit is completed" {
					if len(files) != 1 {
						t.Errorf("Expected 1 file, got %d, %v", len(files), files)
					}
					if !slices.ContainsFunc(files, func(file string) bool {
						return path.Base(file) == msg.ID+".log"
					}) {
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

func TestPublishHonorsCanceledContext(t *testing.T) {
	writer := &Writer{
		openConnections: make(map[string]Connection),
	}
	tmpPath := t.TempDir()
	writer.openConnections["conn1"] = Connection{
		Path:            tmpPath,
		MaxAuditResults: 1,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := writer.Publish(ctx, "conn1", util.ExportMsg{
		ID:      "audit1",
		Message: util.AuditStartedMsg,
	}, "topic1")
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if _, err := os.Stat(path.Join(tmpPath, "topic1")); !os.IsNotExist(err) {
		t.Fatalf("expected no audit directory, got err %v", err)
	}
}

func TestPublishPersistsFileAfterAuditStartMarshalError(t *testing.T) {
	writer := &Writer{
		openConnections: make(map[string]Connection),
	}
	writer.openConnections["conn1"] = Connection{
		Path:            t.TempDir(),
		MaxAuditResults: 1,
	}

	err := writer.Publish(context.Background(), "conn1", util.ExportMsg{
		ID:      "audit1",
		Message: util.AuditStartedMsg,
		Details: make(chan struct{}),
	}, "topic1")
	if err == nil || !strings.Contains(err.Error(), "error marshaling data") {
		t.Fatalf("expected marshaling error, got %v", err)
	}

	writer.mu.Lock()
	conn := writer.openConnections["conn1"]
	writer.mu.Unlock()
	if conn.File == nil {
		t.Fatal("expected opened file to remain tracked after marshal error")
	}

	if err := writer.CloseConnection("conn1"); err != nil {
		t.Fatalf("CloseConnection() error = %v", err)
	}
}

func TestPublishDoesNotTrackFileAfterAuditStartLockError(t *testing.T) {
	originalLockFile := lockFile
	lockFile = func(_ *os.File) error {
		return fmt.Errorf("forced lock failure")
	}
	defer func() {
		lockFile = originalLockFile
	}()

	writer := &Writer{
		openConnections: make(map[string]Connection),
	}
	writer.openConnections["conn1"] = Connection{
		Path:            t.TempDir(),
		MaxAuditResults: 1,
	}

	err := writer.Publish(context.Background(), "conn1", util.ExportMsg{
		ID:      "audit1",
		Message: util.AuditStartedMsg,
	}, "topic1")
	if err == nil || !strings.Contains(err.Error(), "failed to acquire lock") {
		t.Fatalf("expected lock error, got %v", err)
	}

	writer.mu.Lock()
	conn := writer.openConnections["conn1"]
	writer.mu.Unlock()
	if conn.File != nil {
		t.Fatal("expected failed audit start file to remain untracked")
	}

	if err := writer.CloseConnection("conn1"); err != nil {
		t.Fatalf("CloseConnection() error = %v", err)
	}
}

func TestPublishClearsFileAfterAuditEndRenameError(t *testing.T) {
	writer := &Writer{
		openConnections: make(map[string]Connection),
	}
	tmpPath := t.TempDir()
	writer.openConnections["conn1"] = Connection{
		Path:            tmpPath,
		MaxAuditResults: 1,
	}

	if err := writer.Publish(context.Background(), "conn1", util.ExportMsg{
		ID:      "audit1",
		Message: util.AuditStartedMsg,
	}, "topic1"); err != nil {
		t.Fatalf("Publish(audit start) error = %v", err)
	}
	if err := os.Mkdir(path.Join(tmpPath, "topic1", "audit1.log"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	err := writer.Publish(context.Background(), "conn1", util.ExportMsg{
		ID:      "audit1",
		Message: util.AuditCompletedMsg,
	}, "topic1")
	if err == nil || !strings.Contains(err.Error(), "failed to rename file") {
		t.Fatalf("expected rename error, got %v", err)
	}

	writer.mu.Lock()
	conn := writer.openConnections["conn1"]
	writer.mu.Unlock()
	if conn.File != nil {
		t.Fatal("expected closed file to be cleared after audit end rename error")
	}
}

// TestConcurrentPublishUpdateCloseConnection exercises concurrent Publish,
// UpdateConnection, CloseConnection, and CreateConnection calls on the disk
// driver to validate the mutex-based synchronization and prevent data-race
// regressions. This test is meant to be run with -race to catch races.
func TestConcurrentPublishUpdateCloseConnection(t *testing.T) {
	t.Run("concurrent Publish, UpdateConnection, CloseConnection, and CreateConnection", func(t *testing.T) {
		tmpDir := t.TempDir()
		writer := newTestWriter(nil)

		ctx := context.Background()
		config := diskConfig(tmpDir, 5.0)

		const connectionName = "concurrent-test"

		// Create the initial connection so Publish / Update have something to work with.
		requireCreateConnection(t, writer, connectionName, config)

		var wg sync.WaitGroup
		const (
			publishers = 8
			updaters   = 4
			closers    = 4
			creators   = 4
			iterations = 20
		)
		var resultMu sync.Mutex
		var unexpectedErrors []string
		var publishSuccesses, createSuccesses, closeSuccesses int
		recordResult := func(operation string, err error, allowedErrors ...string) {
			resultMu.Lock()
			defer resultMu.Unlock()
			if err == nil {
				switch operation {
				case "Publish":
					publishSuccesses++
				case "CreateConnection":
					createSuccesses++
				case "CloseConnection":
					closeSuccesses++
				}
				return
			}
			for _, allowed := range allowedErrors {
				if strings.Contains(err.Error(), allowed) {
					return
				}
			}
			unexpectedErrors = append(unexpectedErrors, fmt.Sprintf("%s: %v", operation, err))
		}

		for i := 0; i < publishers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					recordResult("Publish", writer.Publish(ctx, connectionName, util.ExportMsg{
						Message: util.AuditStartedMsg,
						ID:      fmt.Sprintf("audit-%d-%d", idx, j),
					}, "audit"), "invalid connection", "audit file already open")
					recordResult("Publish", writer.Publish(ctx, connectionName, util.ExportMsg{
						Message: "test-violation",
					}, "audit"), "invalid connection", "failed to write violation: no file provided")
					recordResult("Publish", writer.Publish(ctx, connectionName, util.ExportMsg{
						Message: util.AuditCompletedMsg,
					}, "audit"), "invalid connection", "failed to write violation: no file provided", "error handling audit end")
				}
			}(i)
		}

		for i := 0; i < updaters; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				updateCfg := map[string]interface{}{
					"path":            tmpDir,
					"maxAuditResults": 5.0,
				}
				for j := 0; j < iterations; j++ {
					recordResult("UpdateConnection", writer.UpdateConnection(ctx, connectionName, updateCfg), "not found")
				}
			}()
		}

		for i := 0; i < closers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					recordResult("CloseConnection", writer.CloseConnection(connectionName), "not found")
				}
			}()
		}

		for i := 0; i < creators; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					recordResult("CreateConnection", writer.CreateConnection(ctx, connectionName, config))
				}
			}()
		}

		wg.Wait()
		resultMu.Lock()
		defer resultMu.Unlock()
		if len(unexpectedErrors) > 0 {
			t.Fatalf("unexpected concurrent operation errors: %v", unexpectedErrors)
		}
		if publishSuccesses == 0 {
			t.Fatal("expected at least one successful Publish call")
		}
		if createSuccesses == 0 {
			t.Fatal("expected at least one successful CreateConnection call")
		}
		if closeSuccesses == 0 {
			t.Fatal("expected at least one successful CloseConnection call")
		}

		writer.mu.Lock()
		connCount := len(writer.openConnections)
		_, hasConn := writer.openConnections[connectionName]
		writer.mu.Unlock()

		if connCount > 1 {
			t.Fatalf("expected at most 1 open connection, got %d", connCount)
		}

		if !hasConn {
			if err := writer.CreateConnection(ctx, connectionName, config); err != nil {
				t.Fatalf("CreateConnection() after concurrent test error = %v", err)
			}
		}
	})
}
