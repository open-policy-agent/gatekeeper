package disk

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// validatePath validates the provided path and returns its cleaned form. It
// performs no filesystem mutations so it is safe to call while parsing config;
// directory creation is handled separately by ensureDirectory.
func validatePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	for _, elem := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if elem == ".." {
			return "", fmt.Errorf("path must not contain '..', dir traversal is not allowed")
		}
	}
	cleanPath := filepath.Clean(path)
	if cleanPath == string(os.PathSeparator) {
		return "", fmt.Errorf("path must not be filesystem root")
	}
	if cleanPath == "." {
		return "", fmt.Errorf("path must not resolve to the current working directory")
	}
	return cleanPath, nil
}

// ensureDirectory creates the directory tree for the given path. It is kept
// separate from validatePath so configuration parsing stays free of side effects.
func ensureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o770); err != nil {
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
	cleanPath, err := validatePath(path)
	if err != nil {
		return "", 0.0, 0, fmt.Errorf("invalid path: %w", err)
	}
	path = cleanPath
	maxResults, maxResultsOk := cfg[maxAuditResults].(float64)
	if !maxResultsOk {
		return "", 0.0, 0, fmt.Errorf("missing or invalid 'maxAuditResults'")
	}
	if maxResults < 0 {
		return "", 0.0, 0, fmt.Errorf("maxAuditResults cannot be negative")
	}
	if maxResults != math.Trunc(maxResults) {
		return "", 0.0, 0, fmt.Errorf("maxAuditResults must be an integer")
	}
	if maxResults > maxAllowedAuditRuns {
		return "", 0.0, 0, fmt.Errorf("maxAuditResults cannot be greater than the maximum allowed audit runs: %d", maxAllowedAuditRuns)
	}
	ttl := maxConnectionAge
	if ttlValue, ok := cfg["closedConnectionTTL"]; ok {
		ttlStr, ok := ttlValue.(string)
		if !ok {
			return "", 0.0, 0, fmt.Errorf("invalid ttl format: expected string")
		}
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

// retryConfig holds the tunable retry parameters for failed-connection
// cleanup. A nil/empty config yields the package defaults so existing
// behavior is preserved.
type retryConfig struct {
	maxRetryAttempts   int
	baseRetryDelay     time.Duration
	retryBackoffFactor float64
	maxRetryDelay      time.Duration
}

// defaultRetryConfig returns the package-level defaults.
func defaultRetryConfig() retryConfig {
	return retryConfig{
		maxRetryAttempts:   maxRetryAttempts,
		baseRetryDelay:     baseRetryDelay,
		retryBackoffFactor: retryBackoffFactor,
		maxRetryDelay:      maxRetryDelay,
	}
}

// parseRetryConfig extracts optional retry tuning from a connection config.
// Any field that is absent or invalid falls back to its package default,
// so partial configuration is safe.
func parseRetryConfig(config interface{}) retryConfig {
	cfg, ok := config.(map[string]interface{})
	rc := defaultRetryConfig()
	if !ok {
		return rc
	}

	if v, ok := cfg["maxRetryAttempts"].(float64); ok && v > 0 && v == math.Trunc(v) {
		rc.maxRetryAttempts = int(v)
	}
	if v, ok := cfg["baseRetryDelay"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			rc.baseRetryDelay = d
		}
	}
	if v, ok := cfg["retryBackoffFactor"].(float64); ok && v > 0 {
		rc.retryBackoffFactor = v
	}
	if v, ok := cfg["maxRetryDelay"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			rc.maxRetryDelay = d
		}
	}
	return rc
}
