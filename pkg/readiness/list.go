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
	"errors"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type listerFunc func(ctx context.Context, out runtime.Object, opts ...client.ListOption) error

func (f listerFunc) List(ctx context.Context, out runtime.Object, opts ...client.ListOption) error {
	return f(ctx, out, opts...)
}

// retryLister returns a delegating lister that retries until it succeeds or its context is canceled.
// Optionally, an isRecoverable function can be provided. When provided, it will be consulted to
// determine if errors are transient or recoverable. Non-recoverable errors will abort the retry loop.
func retryLister(r Lister, isRecoverable func(err error) bool) Lister {
	return listerFunc(func(ctx context.Context, out runtime.Object, opts ...client.ListOption) error {
		if out == nil {
			return errors.New("nil output resource")
		}
		gvk := out.GetObjectKind().GroupVersionKind()

		backoff := retry.DefaultBackoff
		backoff.Cap = 5 * time.Second
		err := syncutil.BackoffWithContext(ctx, backoff, func() (bool, error) {
			err := r.List(ctx, out, opts...)

			if err != nil {
				if ctx.Err() != nil {
					// Give up when our parent context is canceled
					return false, err
				}
				if isRecoverable != nil && !isRecoverable(err) {
					return false, err
				}
				// Log and retry w/ backoff
				log.V(1).Info("transient issue while listing, retrying...", "gvk", gvk, "err", err)
				return false, nil
			}

			// Success
			return true, nil
		})

		if err != nil {
			log.Error(err, "listing", "gvk", gvk, "err", err)
			return err
		}
		return nil
	})
}
