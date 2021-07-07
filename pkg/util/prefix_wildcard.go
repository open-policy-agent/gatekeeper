package util

import "strings"

// PrefixWildcard is a string that supports globbing at its end.
type PrefixWildcard string

// Matches returns true if the candidate parameter is either an exact match of the PrefixWildcard,
// or if the PrefixWildcard is a valid glob-match for the candidate.  The PrefixWildcard must
// end in a "*" to be considered a glob.
func (pw PrefixWildcard) Matches(candidate string) bool {
	return strings.HasPrefix(candidate, strings.TrimSuffix(string(pw), "*"))
}
