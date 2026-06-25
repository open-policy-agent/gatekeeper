package disk

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

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
				writer.closeAndRemoveFilesWithRetry = func(_ *Connection) error {
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
				writer.closeAndRemoveFilesWithRetry = func(_ *Connection) error {
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
				writer.closeAndRemoveFilesWithRetry = func(_ *Connection) error {
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
				writer.closeAndRemoveFilesWithRetry = func(_ *Connection) error {
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
				writer.closeAndRemoveFilesWithRetry = func(_ *Connection) error {
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

				writer.closeAndRemoveFilesWithRetry = func(conn *Connection) error {
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
				mu:                sync.Mutex{},
				openConnections:   make(map[string]Connection),
				closedConnections: make(map[string]FailedConnection),
				cleanupDone:       make(chan struct{}),
				cleanupStopped:    false,
				closeAndRemoveFilesWithRetry: func(_ *Connection) error {
					return nil
				},
			}

			tt.setup(writer)

			writer.retryFailedConnections()

			writer.mu.Lock()
			actualLen := len(writer.closedConnections)
			writer.mu.Unlock()

			if actualLen != tt.expectedClosedConnsLen {
				t.Errorf("expected %d closed connections, got %d", tt.expectedClosedConnsLen, actualLen)
			}

			if tt.validateResult != nil {
				writer.mu.Lock()
				writerCopy := &Writer{
					closedConnections: make(map[string]FailedConnection),
					cleanupStopped:    writer.cleanupStopped,
				}
				for k, v := range writer.closedConnections {
					writerCopy.closedConnections[k] = v
				}
				writer.mu.Unlock()

				tt.validateResult(t, writerCopy)
			}

			writer.mu.Lock()
			cleanupStopped := writer.cleanupStopped
			writer.mu.Unlock()

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
func TestBackgroundCleanupLifecycle(t *testing.T) {
	t.Run("Background cleanup starts on first CreateConnection", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			cleanupOnce:       sync.Once{},
			cleanupStopped:    false,
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		_, exists := writer.openConnections["test-conn"]
		cleanupStopped := writer.cleanupStopped
		writer.mu.Unlock()

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
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			cleanupOnce:       sync.Once{},
			cleanupStopped:    false,
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		cleanupStopped := writer.cleanupStopped
		writer.mu.Unlock()

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
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			cleanupOnce:       sync.Once{},
			cleanupStopped:    false,
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		cleanupStopped := writer.cleanupStopped
		writer.mu.Unlock()

		if !cleanupStopped {
			t.Error("Expected cleanup to be stopped after first phase")
		}

		writer.closeAndRemoveFilesWithRetry = func(_ *Connection) error {
			return fmt.Errorf("simulated failure")
		}

		err = writer.CreateConnection(ctx, "test-conn-2", config)
		if err != nil {
			t.Fatalf("Second CreateConnection failed: %v", err)
		}

		_ = writer.CloseConnection("test-conn-2")

		writer.mu.Lock()
		cleanupStoppedAfterRestart := writer.cleanupStopped
		writer.mu.Unlock()

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
func TestCloseAndRemoveFilesWithBackoffRetriesRemoveAfterClosingFile(t *testing.T) {
	tmpDir := t.TempDir()
	file, err := os.CreateTemp(tmpDir, "cleanup")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("Flock() error = %v", err)
	}

	removeCalls := 0
	conn := &Connection{Path: tmpDir, File: file}
	err = closeAndRemoveFilesWithBackoff(conn, wait.Backoff{
		Steps:    3,
		Duration: time.Nanosecond,
		Factor:   1,
		Jitter:   0,
	}, func(string) error {
		removeCalls++
		if removeCalls == 1 {
			return fmt.Errorf("transient remove failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("closeAndRemoveFilesWithBackoff() error = %v", err)
	}
	if removeCalls != 2 {
		t.Fatalf("expected 2 remove attempts, got %d", removeCalls)
	}
	if conn.File != nil {
		t.Fatal("expected file to remain nil after cleanup succeeds")
	}

	file, err = os.CreateTemp(tmpDir, "cleanup")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("Flock() error = %v", err)
	}

	conn = &Connection{Path: tmpDir, File: file}
	err = closeAndRemoveFilesWithBackoff(conn, wait.Backoff{
		Steps:    1,
		Duration: time.Nanosecond,
		Factor:   1,
		Jitter:   0,
	}, func(string) error {
		return fmt.Errorf("persistent remove failure")
	})
	if err == nil {
		t.Fatal("expected closeAndRemoveFilesWithBackoff() to fail")
	}
	if conn.File != nil {
		t.Fatal("expected file to remain nil after close succeeds but remove fails")
	}
}
func TestCloseConnectionWithFailedRetries(t *testing.T) {
	t.Run("Failed close adds connection to closedConnections", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		_, existsInOpen := writer.openConnections["failing-conn"]
		writer.mu.Unlock()

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

	t.Run("Retry skips cleanup path reused by active connection", func(t *testing.T) {
		cleanupErr := fmt.Errorf("simulated failure")
		writer := newTestWriter(func(conn *Connection) error {
			if conn.Path == "" {
				t.Fatal("cleanup called with empty path")
			}
			return cleanupErr
		})

		connectionName := "reused-path-conn"
		config := diskConfig(t.TempDir(), 5.0)

		requireCreateConnection(t, writer, connectionName, config)
		if err := writer.CloseConnection(connectionName); err == nil {
			t.Fatal("expected CloseConnection() to fail")
		}
		requireCreateConnection(t, writer, connectionName, config)

		writer.closeAndRemoveFilesWithRetry = func(conn *Connection) error {
			if conn.Path == config["path"] {
				t.Fatalf("stale cleanup attempted active path %s", conn.Path)
			}
			return nil
		}

		markClosedConnectionsReady(writer)

		writer.retryFailedConnections()

		if !openConnectionExists(writer, connectionName) {
			t.Fatalf("active connection %s was removed", connectionName)
		}
		if closedConnectionExists(writer, connectionName) {
			t.Fatalf("stale failed connection should be dropped while path is active")
		}
	})

	t.Run("Background cleanup retries failed connections", func(t *testing.T) {
		callCount := 0
		writer := &Writer{
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		_, exists := writer.closedConnections["retry-conn"]
		writer.mu.Unlock()

		if !exists {
			t.Error("Connection should still be in closedConnections after failed retry")
		}

		writer.mu.Lock()
		failedConn := writer.closedConnections["retry-conn"]
		writer.mu.Unlock()

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

		writer.mu.Lock()
		failedConn = writer.closedConnections["retry-conn"]
		writer.mu.Unlock()

		failedConn.NextRetryAt = now.Add(-1 * time.Second)
		writer.mu.Lock()
		writer.closedConnections["retry-conn"] = failedConn
		writer.mu.Unlock()

		writer.retryFailedConnections()

		if callCount != 3 {
			t.Errorf("Expected 3 calls to closeAndRemoveFilesWithRetry, got %d", callCount)
		}

		writer.mu.Lock()
		_, exists = writer.closedConnections["retry-conn"]
		writer.mu.Unlock()

		if exists {
			t.Error("Connection should have been removed from closedConnections after successful retry")
		}
	})

	t.Run("Connections are removed after max retry attempts", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		_, exists := writer.closedConnections["max-retries-conn"]
		writer.mu.Unlock()

		if exists {
			t.Error("Connection should have been removed after exceeding max retry attempts")
		}
	})

	t.Run("Expired connections are removed", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		_, exists := writer.closedConnections["expired-conn"]
		writer.mu.Unlock()

		if exists {
			t.Error("Expired connection should have been removed")
		}
	})
}
func closedConnectionExists(writer *Writer, connName string) bool {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	for name := range writer.closedConnections {
		if strings.Contains(name, connName) {
			return true
		}
	}
	return false
}
func getClosedConnections(writer *Writer, connName string) []FailedConnection {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	var conns []FailedConnection
	for name := range writer.closedConnections {
		if strings.Contains(name, connName) {
			conns = append(conns, writer.closedConnections[name])
		}
	}
	return conns
}
func TestTTLBasedConnectionRemoval(t *testing.T) {
	t.Run("Connection removed after custom TTL expires", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		conn := writer.openConnections["short-ttl-conn"]
		writer.mu.Unlock()

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

		writer.mu.Lock()
		_, stillExists := writer.closedConnections["short-ttl-conn"]
		writer.mu.Unlock()

		if stillExists {
			t.Error("Connection should have been removed after TTL expiration")
		}
	})

	t.Run("Connection with longer TTL remains in queue", func(t *testing.T) {
		writer := &Writer{
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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
		writer.mu.Lock()
		conn := writer.openConnections["long-ttl-conn"]
		conn.ClosedConnectionTTL = 10 * time.Second
		writer.openConnections["long-ttl-conn"] = conn
		writer.mu.Unlock()

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
			mu:                sync.Mutex{},
			openConnections:   make(map[string]Connection),
			closedConnections: make(map[string]FailedConnection),
			cleanupDone:       make(chan struct{}),
			closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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

		writer.mu.Lock()
		shortCount := len(writer.closedConnections)
		writer.mu.Unlock()

		if shortCount != 2 {
			t.Errorf("Expected 2 connections in closedConnections, got %d", shortCount)
		}

		time.Sleep(100 * time.Millisecond)

		writer.retryFailedConnections()
		writer.mu.Lock()
		finalCount := len(writer.closedConnections)
		writer.mu.Unlock()

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

// TestConcurrentPublishUpdateCloseConnection exercises concurrent Publish,
// UpdateConnection, CloseConnection, and CreateConnection calls on the disk
// driver to validate the mutex-based synchronization and prevent data-race
// regressions. This test is meant to be run with -race to catch races.
