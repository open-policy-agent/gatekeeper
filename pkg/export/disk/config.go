package disk

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type connectionConfig struct {
	path                string
	maxAuditResults     int
	closedConnectionTTL time.Duration
}

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
	parsed, err := parseConnectionConfig(config)
	if err != nil {
		return "", 0, 0, err
	}
	return parsed.path, float64(parsed.maxAuditResults), parsed.closedConnectionTTL, nil
}

func parseConnectionConfig(config interface{}) (connectionConfig, error) {
	parsed := connectionConfig{
		closedConnectionTTL: maxConnectionAge,
	}
	cfg, ok := config.(map[string]interface{})
	if !ok {
		return connectionConfig{}, fmt.Errorf("invalid config format, expected map[string]interface{}")
	}

	path, pathOk := cfg[violationPath].(string)
	if !pathOk {
		return connectionConfig{}, fmt.Errorf("missing or invalid 'path'")
	}
	cleanPath, err := validatePath(path)
	if err != nil {
		return connectionConfig{}, fmt.Errorf("invalid path: %w", err)
	}
	parsed.path = cleanPath
	maxResults, maxResultsOk := cfg[maxAuditResults].(float64)
	if !maxResultsOk {
		return connectionConfig{}, fmt.Errorf("missing or invalid 'maxAuditResults'")
	}
	if maxResults < 0 {
		return connectionConfig{}, fmt.Errorf("maxAuditResults cannot be negative")
	}
	if maxResults != math.Trunc(maxResults) {
		return connectionConfig{}, fmt.Errorf("maxAuditResults must be an integer")
	}
	if maxResults > maxAllowedAuditRuns {
		return connectionConfig{}, fmt.Errorf("maxAuditResults cannot be greater than the maximum allowed audit runs: %d", maxAllowedAuditRuns)
	}
	parsed.maxAuditResults = int(maxResults)
	if ttlValue, ok := cfg["closedConnectionTTL"]; ok {
		ttlStr, ok := ttlValue.(string)
		if !ok {
			return connectionConfig{}, fmt.Errorf("invalid ttl format: expected string")
		}
		parsed.closedConnectionTTL, err = time.ParseDuration(ttlStr)
		if err != nil {
			return connectionConfig{}, fmt.Errorf("invalid ttl format: %w", err)
		}
	}
	if parsed.closedConnectionTTL > maxConnectionAge {
		return connectionConfig{}, fmt.Errorf("closedConnectionTTL %s exceeds maximum allowed: %s", parsed.closedConnectionTTL, maxConnectionAge)
	}
	if parsed.closedConnectionTTL < minConnectionAge {
		return connectionConfig{}, fmt.Errorf("closedConnectionTTL %s is too short, must be at least 1 minute", parsed.closedConnectionTTL)
	}

	return parsed, nil
}
