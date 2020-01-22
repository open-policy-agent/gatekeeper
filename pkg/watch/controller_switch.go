package watch

import (
	"sync"
)

type ControllerSwitch struct {
	running     bool
	runningLock sync.RWMutex
}

func newSwitch() *ControllerSwitch {
	return &ControllerSwitch{running: true}
}

func (c *ControllerSwitch) stop() {
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
