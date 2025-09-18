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

// Package main provides a command-line tool for CI readiness verification.
// This tool is designed to verify that Gatekeeper admission controller
// readiness probes are functioning correctly before allowing CI pipeline
// progression, preventing deployment of changes that could cause readiness
// probe failures as documented in issue #4055.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"k8s.io/klog/v2"
)

var (
	endpoint        = flag.String("endpoint", "http://localhost:9090/readyz", "Readiness endpoint URL")
	timeout         = flag.Duration("timeout", 5*time.Second, "HTTP request timeout")
	maxRetries      = flag.Int("max-retries", 10, "Maximum verification attempts")
	retryInterval   = flag.Duration("retry-interval", 2*time.Second, "Time between retries")
	stabilityWindow = flag.Duration("stability-window", 30*time.Second, "Time to verify stable readiness")
	totalTimeout    = flag.Duration("total-timeout", 2*time.Minute, "Total timeout for the entire check")
	verbose         = flag.Bool("verbose", false, "Enable verbose logging")
	help            = flag.Bool("help", false, "Show help information")
)

const (
	exitSuccess = 0
	exitFailure = 1
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	if *help {
		showHelp()
		os.Exit(exitSuccess)
	}

	if *verbose {
		klog.Info("Starting Gatekeeper CI readiness verification")
		klog.Info("Configuration",
			"endpoint", *endpoint,
			"timeout", *timeout,
			"maxRetries", *maxRetries,
			"retryInterval", *retryInterval,
			"stabilityWindow", *stabilityWindow,
			"totalTimeout", *totalTimeout)
	}

	// Create readiness checker configuration
	config := &readiness.CIReadinessConfig{
		Endpoint:        *endpoint,
		Timeout:         *timeout,
		MaxRetries:      *maxRetries,
		RetryInterval:   *retryInterval,
		StabilityWindow: *stabilityWindow,
	}

	checker := readiness.NewCIReadinessChecker(config)

	// Set up context with total timeout
	ctx, cancel := context.WithTimeout(context.Background(), *totalTimeout)
	defer cancel()

	// Perform readiness verification
	if err := checker.CheckReadiness(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "❌ CI Readiness verification failed: %v\n", err)
		if *verbose {
			klog.Errorf("Readiness verification failed: %v", err)
		}
		os.Exit(exitFailure)
	}

	fmt.Println("✅ CI Readiness verification passed successfully")
	if *verbose {
		klog.Info("Readiness verification completed successfully")
	}
	os.Exit(exitSuccess)
}

func showHelp() {
	fmt.Printf(`Gatekeeper CI Readiness Checker

This tool verifies that Gatekeeper admission controller readiness probes are
functioning correctly before allowing CI pipeline progression. It implements
a comprehensive multi-stage verification process to prevent deployment of
changes that could cause readiness probe failures.

Usage:
  readiness-check [flags]

Verification Stages:
  1. Initial Readiness: Attempts to verify readiness with retries
  2. Stability Check: Ensures service remains ready over time window

Flags:
`)
	flag.PrintDefaults()

	fmt.Printf(`
Examples:
  # Basic readiness check with default settings
  readiness-check

  # Check custom endpoint with increased retries
  readiness-check --endpoint=http://gatekeeper:9090/readyz --max-retries=15

  # Quick check with shorter timeouts for fast CI pipelines
  readiness-check --timeout=2s --stability-window=15s --total-timeout=1m

  # Verbose output for debugging
  readiness-check --verbose

Exit Codes:
  0: Readiness verification successful
  1: Readiness verification failed

Related Issues:
  - https://github.com/open-policy-agent/gatekeeper/issues/4060
  - https://github.com/open-policy-agent/gatekeeper/issues/4055
`)
}
