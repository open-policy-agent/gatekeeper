package disk

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

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
				mu:                sync.Mutex{},
				openConnections:   make(map[string]Connection),
				closedConnections: make(map[string]FailedConnection),
				cleanupDone:       make(chan struct{}),
				closeAndRemoveFilesWithRetry: func(_ *Connection) error {
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
				writer.mu.Lock()
				conn, exists := writer.openConnections["test-conn"]
				writer.mu.Unlock()

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
