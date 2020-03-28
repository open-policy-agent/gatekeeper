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

package syncutil

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// BackoffWithContext repeats a condition check with exponential backoff,
// exiting early if the provided context is canceled.
//
// It repeatedly checks the condition and then sleeps, using `backoff.Step()`
// to determine the length of the sleep and adjust Duration and Steps.
// Stops and returns as soon as:
// 1. the condition check returns true or an error, or
// 2. the context is canceled.
// In case (1) the returned error is what the condition function returned.
// In all other cases, ErrWaitTimeout is returned.
//
// Adapted from wait.ExponentialBackoff in https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apimachinery/pkg/util/wait/wait.go
func BackoffWithContext(ctx context.Context, backoff wait.Backoff, condition wait.ConditionFunc) error {
	for ctx.Err() == nil {
		if ok, err := condition(); err != nil || ok {
			return err
		}
		select {
		case <-time.After(backoff.Step()):
		case <-ctx.Done():
			return wait.ErrWaitTimeout
		}
	}
	return wait.ErrWaitTimeout
}
