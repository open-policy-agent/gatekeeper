package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version is the gatekeeper version.
var Version string

// GetUserAgent returns a user agent of the format: gatekeeper/<version> (<goos>/<goarch>) <vcsrevision><-vcsdirty>/<vcstimestamp>.
func GetUserAgent() string {
	vcsrevision := "unknown"
	vcstimestamp := "unknown"
	vcsdirty := ""

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, v := range info.Settings {
			switch v.Key {
			case "vcs.revision":
				vcsrevision = v.Value
			case "vcs.modified":
				if v.Value == "true" {
					vcsdirty = "-dirty"
				}
			case "vcs.time":
				vcstimestamp = v.Value
			}
		}
	}

	return fmt.Sprintf("gatekeeper/%s (%s/%s) %s%s/%s", Version, runtime.GOOS, runtime.GOARCH, vcsrevision, vcsdirty, vcstimestamp)
}
