package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestWriter(cleanup func(*Connection) error) *Writer {
	if cleanup == nil {
		cleanup = func(_ *Connection) error {
			return nil
		}
	}
	return &Writer{
		openConnections:              make(map[string]Connection),
		closedConnections:            make(map[string]FailedConnection),
		cleanupDone:                  make(chan struct{}),
		closeAndRemoveFilesWithRetry: cleanup,
	}
}

func diskConfig(cleanupPath string, maxResults float64) map[string]interface{} {
	return map[string]interface{}{
		"path":            cleanupPath,
		"maxAuditResults": maxResults,
	}
}

func requireCreateConnection(t *testing.T, writer *Writer, connectionName string, config map[string]interface{}) {
	t.Helper()
	if err := writer.CreateConnection(context.Background(), connectionName, config); err != nil {
		t.Fatalf("CreateConnection() error = %v", err)
	}
}

func markClosedConnectionsReady(writer *Writer) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	for name, failedConn := range writer.closedConnections {
		failedConn.NextRetryAt = time.Now().Add(-time.Second)
		writer.closedConnections[name] = failedConn
	}
}

func openConnectionExists(writer *Writer, connectionName string) bool {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	_, exists := writer.openConnections[connectionName]
	return exists
}

func connectionLockExists(writer *Writer, connectionName string) bool {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	_, exists := writer.connectionLocks[connectionName]
	return exists
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
