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

package watch

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func randSuffix(n int) string {
	var out strings.Builder

	for i := 0; i < n; i++ {
		j := rand.Intn(26)
		out.WriteRune(rune('a' + j))
	}
	return out.String()
}

// List lists resources of a particular kind using a watch Manager (informers are shared with dynamic watchers).
func List(ctx context.Context, wm *Manager, gvk schema.GroupVersionKind, cbForEach func(runtime.Object)) error {
	initialPopulation := make(chan event.GenericEvent, 1024)
	name := "list-" + gvk.Kind + randSuffix(8)
	r, err := wm.newSyncRegistrar(name, initialPopulation)
	if err != nil {
		return fmt.Errorf("creating registrar: %w", err)
	}
	defer func() {
		_ = wm.RemoveRegistrar(name)
	}()

	backoff := retry.DefaultBackoff
	backoff.Cap = 5 * time.Second
	err = syncutil.BackoffWithContext(ctx, backoff, func() (bool, error) {
		err := r.AddWatch(gvk)
		if err != nil && err != context.Canceled {
			// Log and retry w/ backoff
			log.V(1).Info("listing", "gvk", gvk, "err", err)
			return false, nil
		}
		if err == context.Canceled {
			// Give up
			return false, err
		}
		// Success
		return true, nil
	})

	if err != nil {
		log.Error(err, "listing", "gvk", gvk, "err", err)
		return err
	}

	// Consume initial set. Channel will be closed once all are received.
loop:
	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		case e, ok := <-initialPopulation:
			if !ok {
				break loop
			}
			if e.Object == nil {
				continue
			}
			// Callback to caller
			cbForEach(e.Object)
		}
	}

	// Done listing.
	_ = r.RemoveWatch(gvk) // Must happen before ExpectationsDone!

	return nil
}
