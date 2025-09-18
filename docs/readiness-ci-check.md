# CI Readiness Probe Status Check

This document describes the implementation of comprehensive readiness probe verification for Gatekeeper CI pipelines, addressing [Issue #4060](https://github.com/open-policy-agent/gatekeeper/issues/4060) and preventing deployment of changes that could cause readiness probe failures as documented in [Issue #4055](https://github.com/open-policy-agent/gatekeeper/issues/4055).

## Overview

The CI readiness check implementation provides a robust, multi-stage verification process that ensures Gatekeeper admission controller readiness probes are functioning correctly before allowing CI pipeline progression. This prevents the deployment of changes that could result in readiness probe failures requiring multiple restart cycles.

## Components

### 1. CI Readiness Checker (`pkg/readiness/ci_checker.go`)

The core readiness verification component that implements:

- **Multi-stage verification process**
- **Configurable retry logic with exponential backoff**
- **Stability verification over time windows**
- **Comprehensive error handling and reporting**
- **Context-aware timeout management**

#### Key Features

```go
type CIReadinessChecker struct {
    endpoint        string        // Readiness endpoint URL
    timeout         time.Duration // HTTP request timeout  
    maxRetries      int          // Maximum verification attempts
    retryInterval   time.Duration // Time between retries
    stabilityWindow time.Duration // Time to verify stable readiness
    client          *http.Client  // Optimized HTTP client
}
```

#### Verification Stages

1. **Initial Readiness**: Attempts to verify readiness with configurable retries
2. **Stability Check**: Ensures service remains ready over specified time window

### 2. Command Line Tool (`cmd/readiness-check/main.go`)

A standalone CLI tool for CI integration that provides:

- **Flexible configuration options**
- **Detailed logging and verbose output**
- **Exit codes for CI pipeline integration**
- **Comprehensive help documentation**

#### Usage Examples

```bash
# Basic readiness check with default settings
readiness-check

# Custom endpoint with increased retries
readiness-check --endpoint=http://gatekeeper:9090/readyz --max-retries=15

# Quick check for fast CI pipelines
readiness-check --timeout=2s --stability-window=15s --total-timeout=1m
```

### 3. GitHub Actions Integration

#### Workflow Integration (`workflow.yaml`)

The implementation adds readiness verification steps to existing CI workflows:

```yaml
- name: Verify Readiness Probe Status
  run: |
    echo "🔍 Verifying Gatekeeper readiness probe status before completing CI..."
    
    # Build and run readiness checker
    go build -o bin/readiness-check ./cmd/readiness-check
    
    # Verify controller readiness with port-forwarding
    kubectl port-forward -n gatekeeper-system pod/$CONTROLLER_POD 9090:9090 &
    ./bin/readiness-check --endpoint=http://localhost:9090/readyz --verbose
```

#### Reusable Workflow (`readiness-check.yaml`)

A dedicated reusable workflow for comprehensive readiness verification:

```yaml
- name: readiness_verification
  uses: ./.github/workflows/readiness-check.yaml
  with:
    kubernetes_version: "1.33.0"
    timeout: "5m"
    max_retries: "15"
```

## Configuration Options

### CI Readiness Checker Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `endpoint` | `http://localhost:9090/readyz` | Readiness endpoint URL |
| `timeout` | `5s` | HTTP request timeout |
| `max-retries` | `10` | Maximum verification attempts |
| `retry-interval` | `2s` | Time between retries |
| `stability-window` | `30s` | Time to verify stable readiness |
| `total-timeout` | `2m` | Total timeout for entire check |

### Workflow Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `kubernetes_version` | `1.33.0` | Kubernetes version for testing |
| `gatekeeper_namespace` | `gatekeeper-system` | Deployment namespace |
| `timeout` | `5m` | Total verification timeout |
| `max_retries` | `15` | Maximum retry attempts |
| `stability_window` | `60s` | Stability verification window |

## Error Handling

The implementation provides comprehensive error handling for common scenarios:

### Network Errors
- Connection refused or timeout
- DNS resolution failures
- Port forwarding issues

### Readiness Failures
- HTTP 500/503 responses
- Intermittent failures
- Service instability

### Context Cancellation
- Timeout handling
- Graceful shutdown
- Resource cleanup

## Testing

### Unit Tests (`pkg/readiness/ci_checker_test.go`)

Comprehensive test coverage including:

- **Configuration validation**
- **HTTP response handling**
- **Retry logic verification**
- **Stability window testing**
- **Error condition simulation**
- **Context cancellation testing**

#### Test Examples

```go
func TestCheckSingleReadiness(t *testing.T) {
    // Tests various HTTP status codes
    // Verifies request headers
    // Validates timeout handling
}

func TestVerifyInitialReadiness(t *testing.T) {
    // Tests retry logic
    // Verifies failure handling
    // Validates attempt counting
}
```

### Integration Testing

The implementation integrates with existing CI test matrices:
- Multiple Kubernetes versions (1.31.6, 1.32.3, 1.33.2)
- Different namespace configurations
- Helm deployment scenarios

## Performance Impact

### Minimal CI Overhead
- Typical readiness check completes in 30-60 seconds
- Parallel execution with other CI tasks
- Early failure detection reduces overall CI time

### Resource Efficiency
- Lightweight HTTP client with connection reuse
- Minimal memory footprint
- Optimized for CI environments

## Monitoring and Observability

### Logging
- Structured logging with appropriate levels
- Detailed error information for debugging
- Progress indicators for CI visibility

### Metrics
- Request duration tracking
- Retry attempt counting
- Success/failure rate monitoring

## Implementation Benefits

### Issue Prevention
- **Proactive Detection**: Catches readiness issues before deployment
- **Reduced Downtime**: Prevents problematic changes from reaching production
- **Faster Recovery**: Early detection enables quicker issue resolution

### CI/CD Enhancement
- **Pipeline Reliability**: Reduces false positives and CI flakiness
- **Developer Experience**: Clear feedback on readiness status
- **Automation**: Eliminates manual readiness verification steps

### Production Stability
- **Deployment Confidence**: Ensures stable deployments
- **Operational Excellence**: Reduces production incidents
- **Monitoring Integration**: Provides deployment health signals

## Migration Guide

### Enabling Readiness Checks

1. **Existing Workflows**: Readiness checks are automatically integrated into standard workflows
2. **Custom Workflows**: Add readiness verification steps as shown in examples
3. **Local Development**: Use `readiness-check` CLI tool for manual verification

### Configuration Customization

Update workflow parameters for specific requirements:

```yaml
- name: Custom Readiness Check
  run: |
    ./bin/readiness-check \
      --max-retries=20 \
      --stability-window=45s \
      --verbose
```

## Related Issues

- [Issue #4060](https://github.com/open-policy-agent/gatekeeper/issues/4060): CI: Check for readiness probe status in CI
- [Issue #4055](https://github.com/open-policy-agent/gatekeeper/issues/4055): Readiness probe failed - multiple restarts solve the problem

## Contributing

### Adding New Features
1. Extend `CIReadinessConfig` for new configuration options
2. Update CLI flags in `cmd/readiness-check/main.go`
3. Add corresponding tests in `ci_checker_test.go`

### Workflow Enhancements
1. Modify workflow YAML files for new verification steps
2. Update documentation with usage examples
3. Test with different Kubernetes versions and configurations