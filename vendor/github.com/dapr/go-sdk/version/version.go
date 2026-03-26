package version

import (
	// Required for go:embed.
	_ "embed"
)

// SDKVersion contains the version of the SDK.
//
//go:embed sdk-version
var SDKVersion string
