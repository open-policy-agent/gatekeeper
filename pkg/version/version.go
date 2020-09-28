package version

import (
	"fmt"
	"runtime"
)

// Vcs is is the commit hash for the binary build
var Vcs string

// Timestamp is the date for the binary build
var Timestamp string

// Version is the gatekeeper version
var Version string

// GetUserAgent returns a user agent of the format: gatekeeper/<version> (<goos>/<goarch>) <vcs>/<timestamp>
func GetUserAgent() string {
	return fmt.Sprintf("gatekeeper/%s (%s/%s) %s/%s", Version, runtime.GOOS, runtime.GOARCH, Vcs, Timestamp)
}
