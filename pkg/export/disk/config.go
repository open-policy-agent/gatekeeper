package disk

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// validatePath checks if the provided path is valid and writable.
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path must not contain '..', dir traversal is not allowed")
	}
	// validate if the path is writable
	if err := os.MkdirAll(path, 0o777); err != nil {
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
	if err := validatePath(path); err != nil {
		return "", 0.0, 0, fmt.Errorf("invalid path: %w", err)
	}
	maxResults, maxResultsOk := cfg[maxAuditResults].(float64)
	if !maxResultsOk {
		return "", 0.0, 0, fmt.Errorf("missing or invalid 'maxAuditResults'")
	}
	if maxResults < 0 {
		return "", 0.0, 0, fmt.Errorf("maxAuditResults cannot be negative")
	}
	if maxResults > maxAllowedAuditRuns {
		return "", 0.0, 0, fmt.Errorf("maxAuditResults cannot be greater than the maximum allowed audit runs: %d", maxAllowedAuditRuns)
	}
	ttl := maxConnectionAge
	if ttlStr, ok := cfg["closedConnectionTTL"].(string); ok {
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
