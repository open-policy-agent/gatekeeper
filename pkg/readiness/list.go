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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type listerFunc func(ctx context.Context, out runtime.Object, opts ...client.ListOption) error

func (f listerFunc) List(ctx context.Context, out runtime.Object, opts ...client.ListOption) error {
	return f(ctx, out, opts...)
}

// retryLister returns a delegating lister that retries until it succeeds or
// its context is canceled.  Optionally, a predicate can be provided to
// determine if errors are transient and the operation should be retried.  If
// the predicate returns false, the error is terminal and the operation will be
// abandoned.  If predicate is nil, all errors are considered recoverable.
func retryLister(r Lister, predicate retryPredicate) Lister {
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
				if predicate != nil && !predicate(err) {
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

// retryPredicate is a function that determines whether an error is recoverable
// in the context of a retryable operation.  If the predicate returns true, the
// operation can be retried. Otherwise, the error is considered terminal.
type retryPredicate func(err error) bool

// retryAll is a retryPredicate that will retry any error.
func retryAll(_ error) bool {
	return true
}

// retryUnlessUnregistered is a retryPredicate that retries all errors except
// *NoResourceMatchError, *NoKindMatchError, e.g. a resource was not registered to
// the RESTMapper.
func retryUnlessUnregistered(err error) bool {
	// NoKindMatchError is non-recoverable, otherwise we'll retry.
	return !meta.IsNoMatchError(err)
}
