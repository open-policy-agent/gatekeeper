/*
Copyright 2024 The Kubernetes Authors.

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

package disk

import (
	"testing"
	"time"
)

// TestScheduleRetryDelayRespectsMax asserts that the scheduled retry delay
// never exceeds maxDelay, even after jitter is applied. wait.Jitter can
// increase the delay by up to Jitter (10%), so the cap must be applied after
// jitter; a cap applied before jitter lets the jittered value escape the bound.
// Run over many iterations so the random jitter reliably exercises the
// upper edge.
func TestScheduleRetryDelayRespectsMax(t *testing.T) {
	const max = 1 * time.Minute
	// Use a base larger than max so the pre-jitter cap is exercised, and a
	// factor of 1 so every attempt (including attempt 0) stays at the base.
	base := 5 * time.Minute
	factor := 1.0

	for attempt := 0; attempt < 5; attempt++ {
		for i := 0; i < 200; i++ {
			got := scheduleRetryDelay(base, factor, max, attempt)
			if got > max {
				t.Fatalf("attempt %d iteration %d: scheduled delay %v exceeds maxDelay %v", attempt, i, got, max)
			}
		}
	}
}

// TestScheduleRetryDelayCapsInitialRetry covers the documented upper bound for
// the very first retry (attempt 0): when baseRetryDelay exceeds maxRetryDelay,
// the first scheduled delay must be clamped to maxRetryDelay.
func TestScheduleRetryDelayCapsInitialRetry(t *testing.T) {
	const max = 1 * time.Minute
	base := 5 * time.Minute
	factor := 2.0

	got := scheduleRetryDelay(base, factor, max, 0)
	if got > max {
		t.Fatalf("initial retry delay %v exceeds maxDelay %v", got, max)
	}
	if got != max {
		t.Fatalf("expected initial retry delay to be clamped to maxDelay %v, got %v", max, got)
	}
}
