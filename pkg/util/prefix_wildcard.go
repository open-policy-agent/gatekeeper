package util

import "strings"

// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\*|-\*)?$`

//nolint:revive
// A string that supports globbing at its end.  Ex: "kube-*" will match "kube-system" or
// "kube-public".  The asterisk is required for wildcard matching.
type PrefixWildcard string

// Matches returns true if the candidate parameter is either an exact match of the PrefixWildcard,
// or if the PrefixWildcard is a valid glob-match for the candidate.  The PrefixWildcard must
// end in a "*" to be considered a glob.
func (pw PrefixWildcard) Matches(candidate string) bool {
	strPW := string(pw)

	if strings.HasSuffix(strPW, "*") {
		return strings.HasPrefix(candidate, strings.TrimSuffix(strPW, "*"))
	}

	return strPW == candidate
}
