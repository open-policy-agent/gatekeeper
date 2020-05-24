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

type Waiter interface {
	Wait() error
}

// WaitAll waits (blocks) for multiple Wait()-ables and returns the first non-nil error.
func WaitAll(w ...Waiter) error {
	var final error
	for _, f := range w {
		if err := f.Wait(); err != nil && final == nil {
			final = err
		}
	}
	return final
}
