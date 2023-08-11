package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

const gatorState = "beta"

// Version is the gatekeeper version.
var Version string

// GetUserAgent returns Gatekeeper and Gator version information.
func GetUserAgent(name string) string {
	vcsrevision := "unknown"
	vcstimestamp := "unknown"
	vcsdirty := ""
	opaVersion := "unknown"
	frameworksVersion := "unknown"

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

		for _, v := range info.Deps {
			switch v.Path {
			case "github.com/open-policy-agent/opa":
				opaVersion = v.Version
			case "github.com/open-policy-agent/frameworks/constraint":
				frameworksVersion = v.Version
			}
		}
	}

	// OPA and Frameworks version used by Gatekeeper and Gator
	opaFrameworksVersion := fmt.Sprintf("opa/%s, frameworks/%s", opaVersion, frameworksVersion)

	// if LDFLAGS are not set, use revision info
	if Version == "" {
		Version = fmt.Sprintf("devel (%s)", vcsrevision)
	}

	if name == "gator" {
		return fmt.Sprintf("%s (Feature State: %s), %s", Version, gatorState, opaFrameworksVersion)
	}

	return fmt.Sprintf("%s/%s (%s/%s) %s%s/%s, %s", name, Version, runtime.GOOS, runtime.GOARCH, vcsrevision, vcsdirty, vcstimestamp, opaFrameworksVersion)
}
