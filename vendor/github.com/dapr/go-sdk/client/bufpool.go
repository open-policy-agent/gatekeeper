/*
Copyright 2023 The Dapr Authors
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

package client

import (
	"sync"
)

// Maximum size, in bytes, for the buffer used by stream invocations: 2KB.
const StreamBufferSize = 2 << 10

// Pool of *[]byte used by stream invocations. Their size is fixed at StreamBufferSize.
var bufPool = sync.Pool{
	New: func() any {
		// Return a pointer here
		// See https://github.com/dominikh/go-tools/issues/1336 for explanation
		b := make([]byte, StreamBufferSize)
		return &b
	},
}
