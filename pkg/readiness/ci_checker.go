/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package readiness

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var ciLog = logf.Log.WithName("ci-readiness-checker")

// CIReadinessChecker provides CI-specific readiness probe verification functionality.
type CIReadinessChecker struct {
	endpoint        string
	timeout         time.Duration
	maxRetries      int
	retryInterval   time.Duration
	stabilityWindow time.Duration
	client          *http.Client
}

// CIReadinessConfig configures the CI readiness checker behavior.
type CIReadinessConfig struct {
	Endpoint        string        // Readiness endpoint URL (default: http://localhost:9090/readyz)
	Timeout         time.Duration // HTTP request timeout (default: 5s)
	MaxRetries      int           // Maximum verification attempts (default: 10)
	RetryInterval   time.Duration // Time between retries (default: 2s)
	StabilityWindow time.Duration // Time to verify stable readiness (default: 30s)
}

// DefaultCIConfig returns a default configuration for CI readiness checking.
func DefaultCIConfig() *CIReadinessConfig {
	return &CIReadinessConfig{
		Endpoint:        "http://localhost:9090/readyz",
		Timeout:         5 * time.Second,
		MaxRetries:      10,
		RetryInterval:   2 * time.Second,
		StabilityWindow: 30 * time.Second,
	}
}

// NewCIReadinessChecker creates a new CI readiness checker with the provided configuration.
func NewCIReadinessChecker(config *CIReadinessConfig) *CIReadinessChecker {
	if config == nil {
		config = DefaultCIConfig()
	}

	client := &http.Client{
		Timeout: config.Timeout,
		// Disable redirects for health checks
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &CIReadinessChecker{
		endpoint:        config.Endpoint,
		timeout:         config.Timeout,
		maxRetries:      config.MaxRetries,
		retryInterval:   config.RetryInterval,
		stabilityWindow: config.StabilityWindow,
		client:          client,
	}
}

// CheckReadiness performs comprehensive readiness verification suitable for CI environments.
// This method implements a multi-stage verification process to ensure stable readiness
// before allowing CI pipeline progression.
func (c *CIReadinessChecker) CheckReadiness(ctx context.Context) error {
	ciLog.Info("starting CI readiness verification", "endpoint", c.endpoint)

	// Stage 1: Initial readiness verification with retries
	if err := c.verifyInitialReadiness(ctx); err != nil {
		return errors.Wrap(err, "initial readiness verification failed")
	}

	// Stage 2: Stability verification over time window
	if err := c.verifyStableReadiness(ctx); err != nil {
		return errors.Wrap(err, "stability verification failed")
	}

	ciLog.Info("CI readiness verification completed successfully")
	return nil
}

// verifyInitialReadiness performs the initial readiness check with retry logic.
func (c *CIReadinessChecker) verifyInitialReadiness(ctx context.Context) error {
	ciLog.Info("verifying initial readiness", "maxRetries", c.maxRetries, "retryInterval", c.retryInterval)

	var lastErr error
	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ready, err := c.checkSingleReadiness(ctx)
		if err != nil {
			lastErr = err
			ciLog.Info("readiness check failed", "attempt", attempt, "error", err.Error())
		} else if ready {
			ciLog.Info("initial readiness verified", "attempt", attempt)
			return nil
		} else {
			lastErr = fmt.Errorf("readiness probe returned not ready")
			ciLog.Info("service not ready", "attempt", attempt)
		}

		if attempt < c.maxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryInterval):
			}
		}
	}

	return errors.Wrapf(lastErr, "readiness verification failed after %d attempts", c.maxRetries)
}

// verifyStableReadiness ensures the service remains ready over the stability window.
func (c *CIReadinessChecker) verifyStableReadiness(ctx context.Context) error {
	ciLog.Info("verifying stable readiness", "stabilityWindow", c.stabilityWindow)

	stabilityCtx, cancel := context.WithTimeout(ctx, c.stabilityWindow)
	defer cancel()

	checkInterval := c.stabilityWindow / 10 // Check 10 times during stability window
	if checkInterval < 100*time.Millisecond {
		checkInterval = 100 * time.Millisecond
	}
	if checkInterval > time.Second {
		checkInterval = time.Second
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	checksPerformed := 0
	for {
		select {
		case <-stabilityCtx.Done():
			if stabilityCtx.Err() == context.DeadlineExceeded {
				ciLog.Info("stability verification completed", "checksPerformed", checksPerformed)
				return nil
			}
			return stabilityCtx.Err()
		case <-ticker.C:
			ready, err := c.checkSingleReadiness(ctx)
			checksPerformed++

			if err != nil {
				return errors.Wrapf(err, "stability check failed during verification (check #%d)", checksPerformed)
			}
			if !ready {
				return fmt.Errorf("service became not ready during stability verification (check #%d)", checksPerformed)
			}

			ciLog.V(1).Info("stability check passed", "checkNumber", checksPerformed)
		}
	}
}

// checkSingleReadiness performs a single readiness probe check.
func (c *CIReadinessChecker) checkSingleReadiness(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return false, errors.Wrap(err, "failed to create readiness request")
	}

	// Set appropriate headers for health check
	req.Header.Set("User-Agent", "gatekeeper-ci-readiness-checker/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return false, errors.Wrap(err, "readiness request failed")
	}
	defer resp.Body.Close()

	// HTTP 200 indicates readiness
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}

	// Log response details for debugging
	ciLog.V(1).Info("readiness check returned non-200 status", "statusCode", resp.StatusCode, "status", resp.Status)
	return false, nil
}

// CheckReadinessWithTimeout is a convenience method that wraps CheckReadiness with a timeout.
func (c *CIReadinessChecker) CheckReadinessWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.CheckReadiness(ctx)
}
