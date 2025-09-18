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
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// PerformanceMetrics tracks webhook performance statistics
type PerformanceMetrics struct {
	mu                sync.RWMutex
	totalRequests     int64
	totalDuration     time.Duration
	maxDuration       time.Duration
	minDuration       time.Duration
	goroutineCount    int64
	memoryUsage       uint64
	lastCleanupTime   time.Time
	requestsPerSecond float64
}

// PerformanceTracker provides webhook performance monitoring and optimization
type PerformanceTracker struct {
	metrics       *PerformanceMetrics
	log           logr.Logger
	enabled       bool
	cleanupTicker *time.Ticker
}

// NewPerformanceTracker creates a new performance tracker for webhook optimization
func NewPerformanceTracker(log logr.Logger, enabled bool) *PerformanceTracker {
	tracker := &PerformanceTracker{
		metrics: &PerformanceMetrics{
			minDuration:     time.Hour, // Initialize with a high value
			lastCleanupTime: time.Now(),
		},
		log:     log.WithName("webhook-performance"),
		enabled: enabled,
	}

	if enabled {
		// Start periodic cleanup and monitoring
		tracker.cleanupTicker = time.NewTicker(30 * time.Second)
		go tracker.periodicCleanup()
		tracker.log.Info("webhook performance tracking enabled")
	}

	return tracker
}

// TrackRequest measures and records performance metrics for a webhook request
func (pt *PerformanceTracker) TrackRequest(ctx context.Context, req admission.Request, handler func(context.Context, admission.Request) admission.Response) admission.Response {
	if !pt.enabled {
		return handler(ctx, req)
	}

	startTime := time.Now()
	startGoroutines := runtime.NumGoroutine()
	
	// Execute the request
	response := handler(ctx, req)
	
	// Record metrics
	duration := time.Since(startTime)
	endGoroutines := runtime.NumGoroutine()
	
	pt.recordMetrics(duration, int64(endGoroutines-startGoroutines))
	
	// Log slow requests
	if duration > 500*time.Millisecond {
		pt.log.Info("slow webhook request detected",
			"duration", duration,
			"operation", req.Operation,
			"kind", req.Kind.Kind,
			"namespace", req.Namespace,
			"name", req.Name,
		)
	}

	return response
}

// recordMetrics updates performance statistics
func (pt *PerformanceTracker) recordMetrics(duration time.Duration, goroutineDelta int64) {
	pt.metrics.mu.Lock()
	defer pt.metrics.mu.Unlock()
	
	pt.metrics.totalRequests++
	pt.metrics.totalDuration += duration
	pt.metrics.goroutineCount += goroutineDelta
	
	if duration > pt.metrics.maxDuration {
		pt.metrics.maxDuration = duration
	}
	
	if duration < pt.metrics.minDuration {
		pt.metrics.minDuration = duration
	}
	
	// Update memory usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	pt.metrics.memoryUsage = m.Alloc
}

// GetMetrics returns current performance metrics
func (pt *PerformanceTracker) GetMetrics() PerformanceMetrics {
	pt.metrics.mu.RLock()
	defer pt.metrics.mu.RUnlock()
	
	metrics := *pt.metrics
	
	// Calculate requests per second
	if pt.metrics.totalDuration > 0 {
		metrics.requestsPerSecond = float64(pt.metrics.totalRequests) / pt.metrics.totalDuration.Seconds()
	}
	
	return metrics
}

// GetAverageDuration returns the average request duration
func (pt *PerformanceTracker) GetAverageDuration() time.Duration {
	pt.metrics.mu.RLock()
	defer pt.metrics.mu.RUnlock()
	
	if pt.metrics.totalRequests == 0 {
		return 0
	}
	
	return pt.metrics.totalDuration / time.Duration(pt.metrics.totalRequests)
}

// periodicCleanup runs periodic maintenance tasks
func (pt *PerformanceTracker) periodicCleanup() {
	for range pt.cleanupTicker.C {
		pt.performCleanup()
	}
}

// performCleanup executes maintenance tasks to optimize performance
func (pt *PerformanceTracker) performCleanup() {
	startTime := time.Now()
	
	// Force garbage collection if memory usage is high
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// If allocated memory > 100MB, trigger GC
	if m.Alloc > 100*1024*1024 {
		pt.log.V(1).Info("triggering garbage collection due to high memory usage",
			"allocatedMB", m.Alloc/(1024*1024))
		runtime.GC()
	}
	
	// Log performance statistics
	metrics := pt.GetMetrics()
	avgDuration := pt.GetAverageDuration()
	
	pt.log.V(1).Info("webhook performance metrics",
		"totalRequests", metrics.totalRequests,
		"averageDuration", avgDuration,
		"maxDuration", metrics.maxDuration,
		"minDuration", metrics.minDuration,
		"requestsPerSecond", fmt.Sprintf("%.2f", metrics.requestsPerSecond),
		"memoryUsageMB", metrics.memoryUsage/(1024*1024),
		"goroutines", runtime.NumGoroutine(),
	)
	
	pt.metrics.mu.Lock()
	pt.metrics.lastCleanupTime = startTime
	pt.metrics.mu.Unlock()
}

// Stop stops the performance tracker and cleanup routines
func (pt *PerformanceTracker) Stop() {
	if pt.cleanupTicker != nil {
		pt.cleanupTicker.Stop()
		pt.log.Info("webhook performance tracking stopped")
	}
}

// OptimizedRequestHandler wraps request handlers with performance optimizations
type OptimizedRequestHandler struct {
	handler   admission.Handler
	tracker   *PerformanceTracker
	log       logr.Logger
	semaphore chan struct{}
}

// NewOptimizedRequestHandler creates a performance-optimized request handler
func NewOptimizedRequestHandler(handler admission.Handler, tracker *PerformanceTracker, log logr.Logger, maxConcurrency int) *OptimizedRequestHandler {
	optimizedHandler := &OptimizedRequestHandler{
		handler: handler,
		tracker: tracker,
		log:     log.WithName("optimized-handler"),
	}
	
	// Set up semaphore for concurrency control
	if maxConcurrency > 0 {
		optimizedHandler.semaphore = make(chan struct{}, maxConcurrency)
		optimizedHandler.log.Info("webhook concurrency control enabled", "maxConcurrency", maxConcurrency)
	}
	
	return optimizedHandler
}

// Handle implements admission.Handler with performance optimizations
func (orh *OptimizedRequestHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Apply concurrency control if enabled
	if orh.semaphore != nil {
		select {
		case orh.semaphore <- struct{}{}:
			defer func() { <-orh.semaphore }()
		case <-ctx.Done():
			return admission.Errored(http.StatusTooManyRequests, 
				fmt.Errorf("webhook request cancelled due to concurrency limit"))
		}
	}
	
	// Track performance metrics
	return orh.tracker.TrackRequest(ctx, req, orh.handler.Handle)
}

// WebhookPerformanceConfig configures webhook performance optimizations
type WebhookPerformanceConfig struct {
	EnableMetrics     bool
	MaxConcurrency    int
	GCMemoryThreshold uint64 // Memory threshold in bytes to trigger GC
	SlowRequestThreshold time.Duration
}

// DefaultWebhookPerformanceConfig returns default performance configuration
func DefaultWebhookPerformanceConfig() *WebhookPerformanceConfig {
	return &WebhookPerformanceConfig{
		EnableMetrics:        true,
		MaxConcurrency:       runtime.GOMAXPROCS(-1) * 2,
		GCMemoryThreshold:    100 * 1024 * 1024, // 100MB
		SlowRequestThreshold: 500 * time.Millisecond,
	}
}

// MemoryOptimizer provides memory optimization utilities for webhooks
type MemoryOptimizer struct {
	log              logr.Logger
	gcThreshold      uint64
	lastGCTime       time.Time
	minGCInterval    time.Duration
}

// NewMemoryOptimizer creates a new memory optimizer
func NewMemoryOptimizer(log logr.Logger, gcThreshold uint64) *MemoryOptimizer {
	return &MemoryOptimizer{
		log:           log.WithName("memory-optimizer"),
		gcThreshold:   gcThreshold,
		lastGCTime:    time.Now(),
		minGCInterval: 10 * time.Second, // Minimum interval between GC calls
	}
}

// CheckAndOptimize checks memory usage and optimizes if necessary
func (mo *MemoryOptimizer) CheckAndOptimize() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Check if we should trigger GC
	if m.Alloc > mo.gcThreshold && time.Since(mo.lastGCTime) > mo.minGCInterval {
		mo.log.V(1).Info("triggering garbage collection for memory optimization",
			"allocatedBytes", m.Alloc,
			"thresholdBytes", mo.gcThreshold,
			"timeSinceLastGC", time.Since(mo.lastGCTime))
		
		runtime.GC()
		mo.lastGCTime = time.Now()
		
		// Read stats again to log improvement
		runtime.ReadMemStats(&m)
		mo.log.V(1).Info("garbage collection completed",
			"allocatedBytesAfterGC", m.Alloc)
	}
}