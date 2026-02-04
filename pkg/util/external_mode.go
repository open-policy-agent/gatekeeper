package util

import "sync"

var (
	skipPodOwnerRef bool
	podOwnerRefMux  sync.RWMutex
)

// SetSkipPodOwnerRef configures whether status resources should skip setting
// Pod OwnerReference. This should be enabled when Gatekeeper is running in
// a mode where the pod does not exist in the target cluster (--external-mode).
func SetSkipPodOwnerRef(skip bool) {
	podOwnerRefMux.Lock()
	defer podOwnerRefMux.Unlock()
	skipPodOwnerRef = skip
}

// ShouldSkipPodOwnerRef returns true if status resources should not set a
// Pod OwnerReference. This is used in external-mode.
func ShouldSkipPodOwnerRef() bool {
	podOwnerRefMux.RLock()
	defer podOwnerRefMux.RUnlock()
	return skipPodOwnerRef
}
