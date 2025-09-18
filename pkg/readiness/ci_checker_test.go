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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultCIConfig(t *testing.T) {
	config := DefaultCIConfig()
	
	assert.Equal(t, "http://localhost:9090/readyz", config.Endpoint)
	assert.Equal(t, 5*time.Second, config.Timeout)
	assert.Equal(t, 10, config.MaxRetries)
	assert.Equal(t, 2*time.Second, config.RetryInterval)
	assert.Equal(t, 30*time.Second, config.StabilityWindow)
}

func TestNewCIReadinessChecker(t *testing.T) {
	tests := []struct {
		name     string
		config   *CIReadinessConfig
		expected *CIReadinessConfig
	}{
		{
			name:     "nil config uses defaults",
			config:   nil,
			expected: DefaultCIConfig(),
		},
		{
			name: "custom config is used",
			config: &CIReadinessConfig{
				Endpoint:        "http://custom:8080/health",
				Timeout:         10 * time.Second,
				MaxRetries:      5,
				RetryInterval:   1 * time.Second,
				StabilityWindow: 15 * time.Second,
			},
			expected: &CIReadinessConfig{
				Endpoint:        "http://custom:8080/health",
				Timeout:         10 * time.Second,
				MaxRetries:      5,
				RetryInterval:   1 * time.Second,
				StabilityWindow: 15 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewCIReadinessChecker(tt.config)
			
			assert.Equal(t, tt.expected.Endpoint, checker.endpoint)
			assert.Equal(t, tt.expected.Timeout, checker.timeout)
			assert.Equal(t, tt.expected.MaxRetries, checker.maxRetries)
			assert.Equal(t, tt.expected.RetryInterval, checker.retryInterval)
			assert.Equal(t, tt.expected.StabilityWindow, checker.stabilityWindow)
			assert.NotNil(t, checker.client)
		})
	}
}

func TestCheckSingleReadiness(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectedReady  bool
		expectedError  bool
	}{
		{
			name:           "200 OK indicates ready",
			statusCode:     http.StatusOK,
			expectedReady:  true,
			expectedError:  false,
		},
		{
			name:           "500 Internal Server Error indicates not ready",
			statusCode:     http.StatusInternalServerError,
			expectedReady:  false,
			expectedError:  false,
		},
		{
			name:           "503 Service Unavailable indicates not ready",
			statusCode:     http.StatusServiceUnavailable,
			expectedReady:  false,
			expectedError:  false,
		},
		{
			name:           "404 Not Found indicates not ready",
			statusCode:     http.StatusNotFound,
			expectedReady:  false,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request headers
				assert.Contains(t, r.Header.Get("User-Agent"), "gatekeeper-ci-readiness-checker")
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			config := &CIReadinessConfig{
				Endpoint: server.URL,
				Timeout:  1 * time.Second,
			}
			checker := NewCIReadinessChecker(config)

			ready, err := checker.checkSingleReadiness(context.Background())

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedReady, ready)
			}
		})
	}
}

func TestCheckSingleReadinessWithNetworkError(t *testing.T) {
	// Use an invalid endpoint to simulate network error
	config := &CIReadinessConfig{
		Endpoint: "http://localhost:99999/readyz",
		Timeout:  100 * time.Millisecond,
	}
	checker := NewCIReadinessChecker(config)

	ready, err := checker.checkSingleReadiness(context.Background())

	assert.Error(t, err)
	assert.False(t, ready)
	assert.Contains(t, err.Error(), "readiness request failed")
}

func TestCheckSingleReadinessWithContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &CIReadinessConfig{
		Endpoint: server.URL,
		Timeout:  1 * time.Second,
	}
	checker := NewCIReadinessChecker(config)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ready, err := checker.checkSingleReadiness(ctx)

	assert.Error(t, err)
	assert.False(t, ready)
}

func TestVerifyInitialReadiness(t *testing.T) {
	tests := []struct {
		name           string
		responses      []int
		maxRetries     int
		expectedError  bool
		expectedAttempts int
	}{
		{
			name:           "succeeds on first attempt",
			responses:      []int{http.StatusOK},
			maxRetries:     3,
			expectedError:  false,
			expectedAttempts: 1,
		},
		{
			name:           "succeeds on second attempt",
			responses:      []int{http.StatusInternalServerError, http.StatusOK},
			maxRetries:     3,
			expectedError:  false,
			expectedAttempts: 2,
		},
		{
			name:           "fails after max retries",
			responses:      []int{http.StatusInternalServerError, http.StatusInternalServerError, http.StatusInternalServerError},
			maxRetries:     3,
			expectedError:  true,
			expectedAttempts: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if attempts < len(tt.responses) {
					w.WriteHeader(tt.responses[attempts])
				} else {
					w.WriteHeader(http.StatusInternalServerError)
				}
				attempts++
			}))
			defer server.Close()

			config := &CIReadinessConfig{
				Endpoint:      server.URL,
				Timeout:       100 * time.Millisecond,
				MaxRetries:    tt.maxRetries,
				RetryInterval: 10 * time.Millisecond,
			}
			checker := NewCIReadinessChecker(config)

			err := checker.verifyInitialReadiness(context.Background())

			if tt.expectedError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), fmt.Sprintf("after %d attempts", tt.maxRetries))
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedAttempts, attempts)
		})
	}
}

func TestVerifyStableReadiness(t *testing.T) {
	tests := []struct {
		name           string
		responses      []int
		stabilityWindow time.Duration
		expectedError  bool
	}{
		{
			name:           "maintains readiness throughout stability window",
			responses:      []int{http.StatusOK}, // Will repeat this response
			stabilityWindow: 50 * time.Millisecond,
			expectedError:  false,
		},
		{
			name:           "fails when service becomes not ready",
			responses:      []int{http.StatusOK, http.StatusOK, http.StatusInternalServerError},
			stabilityWindow: 100 * time.Millisecond,
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				responseIndex := attempts
				if responseIndex >= len(tt.responses) {
					responseIndex = len(tt.responses) - 1
				}
				w.WriteHeader(tt.responses[responseIndex])
				attempts++
			}))
			defer server.Close()

			config := &CIReadinessConfig{
				Endpoint:        server.URL,
				Timeout:         100 * time.Millisecond,
				StabilityWindow: tt.stabilityWindow,
			}
			checker := NewCIReadinessChecker(config)

			err := checker.verifyStableReadiness(context.Background())

			if tt.expectedError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "stability")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &CIReadinessConfig{
		Endpoint:        server.URL,
		Timeout:         100 * time.Millisecond,
		MaxRetries:      2,
		RetryInterval:   10 * time.Millisecond,
		StabilityWindow: 50 * time.Millisecond,
	}
	checker := NewCIReadinessChecker(config)

	err := checker.CheckReadiness(context.Background())
	assert.NoError(t, err)
}

func TestCheckReadinessWithTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &CIReadinessConfig{
		Endpoint:        server.URL,
		Timeout:         100 * time.Millisecond,
		MaxRetries:      2,
		RetryInterval:   10 * time.Millisecond,
		StabilityWindow: 50 * time.Millisecond,
	}
	checker := NewCIReadinessChecker(config)

	err := checker.CheckReadinessWithTimeout(200 * time.Millisecond)
	assert.NoError(t, err)
}

func TestCheckReadinessWithTimeoutExpired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Simulate slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &CIReadinessConfig{
		Endpoint:        server.URL,
		Timeout:         300 * time.Millisecond,
		MaxRetries:      1,
		RetryInterval:   10 * time.Millisecond,
		StabilityWindow: 100 * time.Millisecond,
	}
	checker := NewCIReadinessChecker(config)

	err := checker.CheckReadinessWithTimeout(50 * time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}