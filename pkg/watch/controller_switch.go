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
	"sync"
)

type ControllerSwitch struct {
	running     bool
	runningLock sync.RWMutex
}

func NewSwitch() *ControllerSwitch {
	return &ControllerSwitch{running: true}
}

func (c *ControllerSwitch) Stop() {
	c.runningLock.Lock()
	defer c.runningLock.Unlock()
	c.running = false
}

func (c *ControllerSwitch) Enter() bool {
	c.runningLock.RLock()
	return c.running
}

func (c *ControllerSwitch) Exit() {
	c.runningLock.RUnlock()
}
