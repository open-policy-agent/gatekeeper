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

import "sync/atomic"

// SyncBool represents a synchronized boolean flag.
// Its methods are safe to call concurrently.
type SyncBool struct {
	val int32
}

// Set sets the values of the flag.
func (b *SyncBool) Set(v bool) {
	if v {
		atomic.StoreInt32(&b.val, 1)
	} else {
		atomic.StoreInt32(&b.val, 0)
	}
}

// Get returns the current value of the flag.
func (b *SyncBool) Get() bool {
	v := atomic.LoadInt32(&b.val)
	return v == 1
}
