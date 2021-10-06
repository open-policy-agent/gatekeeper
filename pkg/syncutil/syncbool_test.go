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
	"k8s.io/apimachinery/pkg/util/wait"
)

// Verifies changes are visible across goroutines.
func Test_SyncBool(t *testing.T) {
	var b syncutil.SyncBool

	got := b.Get()
	if got {
		t.Fatalf("got default SyncBool value to be %t, want %t", got, false)
	}

	go func() {
		b.Set(true)
	}()

	waitErr := wait.Poll(10*time.Millisecond, 5*time.Second, func() (done bool, err error) {
		return b.Get(), nil
	})

	if waitErr != nil {
		// This probably means we timed out waiting for the condition to be set.
		t.Fatalf("got wait.Poll() error = %v, want nil", waitErr)
	}

	if !b.Get() {
		// Sanity check that our wait.Poll actually waited for b to be set to true.
		t.Errorf("wait.Poll succeeded but b.Get() was false")
	}
}
