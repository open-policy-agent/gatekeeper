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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// RetryClient wraps a client to provide rate-limiter respecting retry behavior.
type RetryClient struct {
	client.Client
}

// retry will run the provided function, retrying if it fails due to rate limiting.
// It will respect the rate limiters delay guidance. If context is cancelled, it will
// return early.
func retry(ctx context.Context, f func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := f()
		if delay, needDelay := apiutil.DelayIfRateLimited(err); needDelay {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				continue
			}
		}
		return err
	}
}

func (c *RetryClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	return retry(ctx, func() error {
		return c.Client.Get(ctx, key, obj)
	})
}

func (c *RetryClient) List(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
	return retry(ctx, func() error {
		return c.Client.List(ctx, list, opts...)
	})
}

func (c *RetryClient) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	return retry(ctx, func() error {
		return c.Client.Create(ctx, obj, opts...)
	})
}

func (c *RetryClient) Delete(ctx context.Context, obj runtime.Object, opts ...client.DeleteOption) error {
	return retry(ctx, func() error {
		return c.Client.Delete(ctx, obj, opts...)
	})
}

func (c *RetryClient) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	return retry(ctx, func() error {
		return c.Client.Update(ctx, obj, opts...)
	})
}

func (c *RetryClient) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	return retry(ctx, func() error {
		return c.Client.Patch(ctx, obj, patch, opts...)
	})
}

func (c *RetryClient) DeleteAllOf(ctx context.Context, obj runtime.Object, opts ...client.DeleteAllOfOption) error {
	return retry(ctx, func() error {
		return c.Client.DeleteAllOf(ctx, obj, opts...)
	})
}

func (c *RetryClient) Status() client.StatusWriter {
	return &RetryStatusWriter{c.Client.Status()}
}

type RetryStatusWriter struct {
	client.StatusWriter
}

func (c *RetryStatusWriter) Update(ctx context.Context, obj runtime.Object, opts ...client.UpdateOption) error {
	return retry(ctx, func() error {
		return c.StatusWriter.Update(ctx, obj, opts...)
	})
}

func (c *RetryStatusWriter) Patch(ctx context.Context, obj runtime.Object, patch client.Patch, opts ...client.PatchOption) error {
	return retry(ctx, func() error {
		return c.StatusWriter.Patch(ctx, obj, patch, opts...)
	})
}
