package v1beta1

import (
	"fmt"
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

// dashExtractor unpacks the status resource name, unescaping `-`
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

// dashPacker puts a list of strings into a dash-separated format. Note that
// it cannot handle empty strings, as that makes the dash separator for the empty
// string reduce to an escaped dash. This is fine because none of the packed strings
// are allowed to be empty. If this changes in the future, we could create a placeholder
// for the empty string, say `b`, and replace all instances of `b` in the input
// stream with `bb`, which could then be unfolded. If we need that, we are already
// changing the schema of the status resource, and therefore don't need to deal with
// it now. It also doesn't handle the case where a value begins or ends with a dash,
// which is also disallowed by the schema (and would require an additional placeholder
// character to fix). Finally, note that it is impossible to distinguish between
// a nil list of strings and a list of one empty string.
func dashPacker(vals ...string) (string, error) {
	if len(vals) == 0 {
		return "", fmt.Errorf("dashPacker cannot pack an empty list of strings")
	}
	b := strings.Builder{}
	for i, val := range vals {
		if strings.HasPrefix(val, "-") || strings.HasSuffix(val, "-") {
			return "", fmt.Errorf("dashPacker cannot pack strings that begin or end with a dash: %+v", vals)
		}
		if len(val) == 0 {
			return "", fmt.Errorf("dashPacker cannot pack empty strings")
		}
		if i != 0 {
			b.WriteString("-")
		}
		b.WriteString(strings.Replace(val, "-", "--", -1))
	}
	return b.String(), nil
}
