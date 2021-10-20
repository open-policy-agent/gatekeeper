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

package syncutil_test

import (
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/syncutil"
)

// Verifies changes are visible across goroutines.
func Test_SyncBool(t *testing.T) {
	var b syncutil.SyncBool
	done := make(chan struct{})

	go func() {
		defer close(done)

		for b.Get() == false {
		}
	}()

	go func() {
		b.Set(true)
	}()

	select {
	case <-time.After(10 * time.Millisecond):
		t.Errorf("failed waiting for flag visibility across goroutines")
	case <-done:
		// Success
	}

	if !b.Get() {
		t.Errorf("channel closed but b.Get() was false")
	}
}
