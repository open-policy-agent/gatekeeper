package v1beta1

import (
	"strings"
	"sync"
)

var (
	podOwnershipEnabled = true
	ownerMutex          = sync.RWMutex{}
)

// DisablePodOwnership disables setting the owner reference for Status resource.
// This should only be used for testing, where a Pod resource may not be available
func DisablePodOwnership() {
	ownerMutex.Lock()
	defer ownerMutex.Unlock()
	podOwnershipEnabled = false
}

func PodOwnershipEnabled() bool {
	ownerMutex.RLock()
	defer ownerMutex.RUnlock()
	return podOwnershipEnabled
}

// dashUnpacker unpacks the status resource name, unescaping `-`
func dashExtractor(val string) []string {
	b := strings.Builder{}
	var tokens []string
	var prevDash bool
	for _, chr := range val {
		if prevDash && chr != '-' {
			tokens = append(tokens, b.String())
			b.Reset()
			prevDash = false
		}
		if chr == '-' {
			if prevDash {
				b.WriteRune(chr)
				prevDash = false
				continue
			} else {
				prevDash = true
				continue
			}
		}
		b.WriteRune(chr)
	}
	tokens = append(tokens, b.String())
	return tokens
}

func dashPacker(vals ...string) string {
	b := strings.Builder{}
	for i, val := range vals {
		if i != 0 {
			b.WriteString("-")
		}
		b.WriteString(strings.Replace(val, "-", "--", -1))
	}
	return b.String()
}
