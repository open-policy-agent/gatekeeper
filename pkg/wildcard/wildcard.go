package wildcard

import "strings"

// +kubebuilder:validation:Pattern=`^(\*|\*-)?[a-z0-9]([-:a-z0-9]*[a-z0-9])?(\*|-\*)?$`

// A string that supports globbing at its front or end. Ex: "kube-*" will match "kube-system" or
// "kube-public", "*-system" will match "kube-system" or "gatekeeper-system".  The asterisk is
// required for wildcard matching.
//
//nolint:revive
type Wildcard string

// Matches returns true if the candidate parameter is either an exact match of the Wildcard,
// or if the Wildcard is a valid glob-match for the candidate.  The Wildcard must start or end
// in a "*" to be considered a glob.
func (w Wildcard) Matches(candidate string) bool {
	wStr := string(w)
	switch {
	case strings.HasPrefix(wStr, "*") && strings.HasSuffix(wStr, "*"):
		return strings.Contains(candidate, strings.TrimSuffix(strings.TrimPrefix(wStr, "*"), "*"))
	case strings.HasPrefix(wStr, "*"):
		return strings.HasSuffix(candidate, strings.TrimPrefix(wStr, "*"))
	case strings.HasSuffix(wStr, "*"):
		return strings.HasPrefix(candidate, strings.TrimSuffix(wStr, "*"))
	default:
		return wStr == candidate
	}
}

func (w Wildcard) MatchesGenerateName(candidate string) bool {
	wStr := string(w)
	switch {
	case strings.HasPrefix(wStr, "*") && strings.HasSuffix(wStr, "*"):
		return strings.Contains(candidate, strings.TrimSuffix(strings.TrimPrefix(wStr, "*"), "*"))
	case strings.HasSuffix(wStr, "*"):
		return strings.HasPrefix(candidate, strings.TrimSuffix(wStr, "*"))
	default:
		return false
	}
}
