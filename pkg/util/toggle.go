package util

import "sync"

type Toggle struct {
	enabled bool
	mux     sync.RWMutex
}

func NewToggle() *Toggle {
	return &Toggle{enabled: true}
}

func (t *Toggle) Enabled() bool {
	t.mux.RLock()
	defer t.mux.RUnlock()
	return t.enabled
}

func (t *Toggle) Disable() {
	t.mux.Lock()
	defer t.mux.Unlock()
	t.enabled = false
}
