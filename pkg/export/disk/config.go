package disk

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// validatePath checks if the provided path is valid and ensures the directory exists.
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
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("path must be absolute")
	}
	if cleanPath == string(os.PathSeparator) {
		return "", fmt.Errorf("path must not be filesystem root")
	}
	// ensure the directory exists
	if err := os.MkdirAll(cleanPath, 0o770); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}
	return cleanPath, nil
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
