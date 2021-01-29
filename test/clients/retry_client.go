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

package clients

import (
	"context"

	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// defaultRefilRate is the default rate at which potential calls are
	// added back to the "bucket" of allowed calls.
	defaultRefillRate = 5
	// defaultLimitSize is the default starting/max number of potential calls
	// per second.  Once a call is used, it's added back to the bucket at a rate
	// of defaultRefillRate per second.
	defaultLimitSize = 5
)

// RetryClient wraps a client to provide rate-limiter respecting retry behavior.
type RetryClient struct {
	Limiter *rate.Limiter
	client.Client
}

func NewRetryClient(c client.Client) *RetryClient {
	return &RetryClient{
		Client:  c,
		Limiter: rate.NewLimiter(rate.Limit(defaultRefillRate), defaultLimitSize),
	}
}

// retry will run the provided function, retrying if it fails due to rate limiting.
// If context is cancelled, it will return early.
func retry(ctx context.Context, limiter *rate.Limiter, f func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := f()

		if meta.IsNoMatchError(err) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				_ = limiter.Wait(ctx)
				continue
			}
		}
		return err
	}
}

func (c *RetryClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return retry(ctx, c.Limiter, func() error {
		return c.Client.Get(ctx, key, obj)
	})
}

func (c *RetryClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.Client.List(ctx, list, opts...)
	})
}

func (c *RetryClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.Client.Create(ctx, obj, opts...)
	})
}

func (c *RetryClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.Client.Delete(ctx, obj, opts...)
	})
}

func (c *RetryClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.Client.Update(ctx, obj, opts...)
	})
}

func (c *RetryClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.Client.Patch(ctx, obj, patch, opts...)
	})
}

func (c *RetryClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.Client.DeleteAllOf(ctx, obj, opts...)
	})
}

func (c *RetryClient) Status() client.StatusWriter {
	return &RetryStatusWriter{StatusWriter: c.Client.Status(), Limiter: c.Limiter}
}

type RetryStatusWriter struct {
	client.StatusWriter
	Limiter *rate.Limiter
}

func (c *RetryStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.StatusWriter.Update(ctx, obj, opts...)
	})
}

func (c *RetryStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return retry(ctx, c.Limiter, func() error {
		return c.StatusWriter.Patch(ctx, obj, patch, opts...)
	})
}
