package disk

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

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

func TestHandleAuditStartRejectsUnsafePathSegments(t *testing.T) {
	for _, tt := range []struct {
		name    string
		auditID string
		topic   string
	}{
		{name: "invalid topic traversal", auditID: "audit1", topic: "../topic"},
		{name: "invalid audit traversal", auditID: "../audit", topic: "topic1"},
		{name: "invalid audit separator", auditID: "nested/audit", topic: "topic1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conn := Connection{Path: t.TempDir()}
			err := conn.handleAuditStart(tt.auditID, tt.topic)
			if err == nil || !strings.Contains(err.Error(), "single path segment") {
				t.Fatalf("expected path segment error, got %v", err)
			}
			if conn.File != nil {
				t.Fatal("expected no file to be opened")
			}
		})
	}
}

func TestHandleAuditStartRejectsRepeatedStart(t *testing.T) {
	conn := Connection{Path: t.TempDir()}
	if err := conn.handleAuditStart("audit1", "topic1"); err != nil {
		t.Fatalf("handleAuditStart() error = %v", err)
	}
	defer conn.unlockAndCloseFile()

	err := conn.handleAuditStart("audit2", "topic1")
	if err == nil || !strings.Contains(err.Error(), "audit file already open") {
		t.Fatalf("expected repeated start error, got %v", err)
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
				extraFilePath := path.Join(dir, "extra_audit.log")
				extraFile, err := os.Create(extraFilePath)
				if err != nil {
					return err
				}
				if err := extraFile.Close(); err != nil {
					return err
				}
				oldTime := time.Now().Add(-time.Hour)
				if err := os.Chtimes(extraFilePath, oldTime, oldTime); err != nil {
					return err
				}
				file, err := os.Create(path.Join(dir, conn.currentAuditRun+".txt"))
				if err != nil {
					return err
				}
				conn.File = file
				return os.Chtimes(file.Name(), time.Now(), time.Now())
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
				if tt.expectedFile != "" && !slices.ContainsFunc(files, func(file string) bool {
					return path.Base(file) == tt.expectedFile
				}) {
					t.Errorf("Expected file %s to exist, but it does not. Files: %v", tt.expectedFile, files)
				}
			}
		})
	}
}

func TestHandleAuditEndSetsSharedWriteMode(t *testing.T) {
	conn := Connection{Path: t.TempDir(), MaxAuditResults: 1}
	if err := conn.handleAuditStart("audit1", "topic1"); err != nil {
		t.Fatalf("handleAuditStart() error = %v", err)
	}
	if _, err := conn.File.WriteString("test\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := conn.handleAuditEnd("topic1"); err != nil {
		t.Fatalf("handleAuditEnd() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(conn.Path, "topic1", "audit1.log"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o666 {
		t.Fatalf("expected mode 0666, got %o", got)
	}
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
					if _, err := os.Create(path.Join(dir, fmt.Sprintf("audit%d.log", i))); err != nil {
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
					if _, err := os.Create(path.Join(dir, fmt.Sprintf("audit%d.log", i))); err != nil {
						return err
					}
				}
				if _, err := os.Create(path.Join(dir, "in-progress.txt")); err != nil {
					return err
				}
				if err := os.Mkdir(path.Join(dir, "nested"), 0o755); err != nil {
					return err
				}
				if _, err := os.Create(path.Join(dir, "nested", "nested.log")); err != nil {
					return err
				}
				return nil
			},
			expectError:   false,
			expectedFiles: 4,
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
func TestGetLogFilesSortedByModTimeAsc(t *testing.T) {
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
				_, err := os.Create(path.Join(dir, "file1.log"))
				return err
			},
			expectedFile:  "file1.log",
			expectedFiles: 1,
			expectError:   false,
		},
		{
			name: "Multiple files in directory",
			setup: func(dir string) error {
				for i := 1; i <= 3; i++ {
					if _, err := os.Create(path.Join(dir, fmt.Sprintf("file%d.log", i))); err != nil {
						return err
					}
				}
				if _, err := os.Create(path.Join(dir, "ignored.txt")); err != nil {
					return err
				}
				return nil
			},
			expectedFile:  "file1.log",
			expectedFiles: 3,
			expectError:   false,
		},
		{
			name: "Nested directories ignored",
			setup: func(dir string) error {
				subDir := path.Join(dir, "subdir")
				if err := os.Mkdir(subDir, 0o755); err != nil {
					return err
				}
				if _, err := os.Create(path.Join(subDir, "file1.log")); err != nil {
					return err
				}
				return nil
			},
			expectedFile:  "",
			expectedFiles: 0,
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
			files, err := getLogFilesSortedByModTimeAsc(dir)
			if (err != nil) != tt.expectError {
				t.Errorf("getLogFilesSortedByModTimeAsc() error = %v, expectError %v", err, tt.expectError)
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
