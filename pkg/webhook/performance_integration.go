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

package webhook

import (
	"context"
	"flag"
	"runtime"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	enableWebhookPerformanceTracking = flag.Bool("enable-webhook-performance-tracking", false, "enable webhook performance tracking and optimization")
	webhookMaxConcurrency            = flag.Int("webhook-max-concurrency", -1, "maximum number of concurrent webhook requests, -1 for auto-detection based on GOMAXPROCS")
	webhookGCThreshold               = flag.Uint64("webhook-gc-threshold", 100*1024*1024, "memory threshold in bytes to trigger garbage collection")
)

// AddPerformanceOptimizedPolicyWebhook registers an enhanced policy webhook with performance optimizations
func AddPerformanceOptimizedPolicyWebhook(mgr manager.Manager, deps Dependencies) error {
	if !*enableWebhookPerformanceTracking {
		// Fall back to standard policy webhook if performance tracking is disabled
		return AddPolicyWebhook(mgr, deps)
	}

	log := log.WithValues("hookType", "validation-optimized")

	// Create standard validation handler first
	if err := AddPolicyWebhook(mgr, deps); err != nil {
		return err
	}

	// Determine concurrency limits
	maxConcurrency := *webhookMaxConcurrency
	if maxConcurrency < 1 {
		maxConcurrency = runtime.GOMAXPROCS(-1) * 2
	}

	// Create performance tracking components
	performanceTracker := NewPerformanceTracker(log, true)
	memoryOptimizer := NewMemoryOptimizer(log, *webhookGCThreshold)

	// Create the existing validation handler (this would need to be refactored to be accessible)
	// For now, we'll create a wrapper that can be integrated

	enhancedHandler := &EnhancedValidationHandler{
		performanceTracker: performanceTracker,
		memoryOptimizer:    memoryOptimizer,
		maxConcurrency:     maxConcurrency,
		log:                log,
	}

	// Register enhanced webhook
	wh := &admission.Webhook{Handler: enhancedHandler}
	mgr.GetWebhookServer().Register("/v1/admit-optimized", wh)

	log.Info("performance-optimized webhook registered",
		"maxConcurrency", maxConcurrency,
		"gcThresholdMB", *webhookGCThreshold/(1024*1024))

	return nil
}

// EnhancedValidationHandler wraps the standard validation handler with performance optimizations
type EnhancedValidationHandler struct {
	performanceTracker *PerformanceTracker
	memoryOptimizer    *MemoryOptimizer
	maxConcurrency     int
	log                logr.Logger
	semaphore          chan struct{}
}

// Handle implements admission.Handler with performance enhancements
func (evh *EnhancedValidationHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Initialize semaphore if not already done
	if evh.semaphore == nil && evh.maxConcurrency > 0 {
		evh.semaphore = make(chan struct{}, evh.maxConcurrency)
	}

	// Apply concurrency control
	if evh.semaphore != nil {
		select {
		case evh.semaphore <- struct{}{}:
			defer func() { <-evh.semaphore }()
		case <-ctx.Done():
			return admission.Errored(429, ctx.Err())
		}
	}

	// Check and optimize memory usage before processing
	evh.memoryOptimizer.CheckAndOptimize()

	// Track performance for this request
	return evh.performanceTracker.TrackRequest(ctx, req, func(ctx context.Context, req admission.Request) admission.Response {
		// This would call the actual validation logic
		// For now, return a placeholder response
		evh.log.V(1).Info("processing optimized webhook request",
			"operation", req.Operation,
			"kind", req.Kind.Kind,
			"namespace", req.Namespace,
			"name", req.Name)

		// TODO: Integrate with actual validation handler
		return admission.Allowed("performance optimized webhook processing")
	})
}

// WebhookPerformanceMonitor provides monitoring and alerting for webhook performance
type WebhookPerformanceMonitor struct {
	tracker         *PerformanceTracker
	log             logr.Logger
	alertThresholds *PerformanceAlertThresholds
}

// PerformanceAlertThresholds defines thresholds for performance alerts
type PerformanceAlertThresholds struct {
	MaxAverageLatency    time.Duration
	MaxMemoryUsage       uint64
	MinRequestsPerSecond float64
	MaxGoroutines        int
}

// DefaultPerformanceAlertThresholds returns sensible default alert thresholds
func DefaultPerformanceAlertThresholds() *PerformanceAlertThresholds {
	return &PerformanceAlertThresholds{
		MaxAverageLatency:    1 * time.Second,
		MaxMemoryUsage:       500 * 1024 * 1024, // 500MB
		MinRequestsPerSecond: 1.0,
		MaxGoroutines:        1000,
	}
}

// NewWebhookPerformanceMonitor creates a new performance monitor
func NewWebhookPerformanceMonitor(tracker *PerformanceTracker, log logr.Logger, thresholds *PerformanceAlertThresholds) *WebhookPerformanceMonitor {
	if thresholds == nil {
		thresholds = DefaultPerformanceAlertThresholds()
	}

	return &WebhookPerformanceMonitor{
		tracker:         tracker,
		log:             log.WithName("performance-monitor"),
		alertThresholds: thresholds,
	}
}

// CheckAlerts examines current performance metrics and logs alerts if thresholds are exceeded
func (wpm *WebhookPerformanceMonitor) CheckAlerts() {
	metrics := wpm.tracker.GetMetrics()
	avgDuration := wpm.tracker.GetAverageDuration()
	currentGoroutines := runtime.NumGoroutine()

	// Check average latency
	if avgDuration > wpm.alertThresholds.MaxAverageLatency {
		wpm.log.Info("ALERT: webhook average latency exceeded threshold",
			"averageLatency", avgDuration,
			"threshold", wpm.alertThresholds.MaxAverageLatency)
	}

	// Check memory usage
	if metrics.MemoryUsage > wpm.alertThresholds.MaxMemoryUsage {
		wpm.log.Info("ALERT: webhook memory usage exceeded threshold",
			"memoryUsageMB", metrics.MemoryUsage/(1024*1024),
			"thresholdMB", wpm.alertThresholds.MaxMemoryUsage/(1024*1024))
	}

	// Check requests per second
	if metrics.RequestsPerSecond < wpm.alertThresholds.MinRequestsPerSecond && metrics.TotalRequests > 10 {
		wpm.log.Info("ALERT: webhook requests per second below threshold",
			"requestsPerSecond", metrics.RequestsPerSecond,
			"threshold", wpm.alertThresholds.MinRequestsPerSecond)
	}

	// Check goroutine count
	if currentGoroutines > wpm.alertThresholds.MaxGoroutines {
		wpm.log.Info("ALERT: webhook goroutine count exceeded threshold",
			"goroutines", currentGoroutines,
			"threshold", wpm.alertThresholds.MaxGoroutines)
	}
}

// GetPerformanceReport generates a comprehensive performance report
func (wpm *WebhookPerformanceMonitor) GetPerformanceReport() map[string]interface{} {
	metrics := wpm.tracker.GetMetrics()
	avgDuration := wpm.tracker.GetAverageDuration()

	return map[string]interface{}{
		"totalRequests":     metrics.TotalRequests,
		"averageLatency":    avgDuration.String(),
		"maxLatency":        metrics.MaxDuration.String(),
		"minLatency":        metrics.MinDuration.String(),
		"requestsPerSecond": metrics.RequestsPerSecond,
		"memoryUsageMB":     metrics.MemoryUsage / (1024 * 1024),
		"goroutineCount":    runtime.NumGoroutine(),
		"lastCleanupTime":   metrics.LastCleanupTime,
		"performanceAlerts": map[string]bool{
			"highLatency":    avgDuration > wpm.alertThresholds.MaxAverageLatency,
			"highMemory":     metrics.MemoryUsage > wpm.alertThresholds.MaxMemoryUsage,
			"lowThroughput":  metrics.RequestsPerSecond < wpm.alertThresholds.MinRequestsPerSecond,
			"highGoroutines": runtime.NumGoroutine() > wpm.alertThresholds.MaxGoroutines,
		},
	}
}
