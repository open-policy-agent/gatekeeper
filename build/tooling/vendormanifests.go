// This is used for vendoring frameworks to Gatekeeper.

//go:build tools

package build

import (
	_ "github.com/open-policy-agent/frameworks/constraint/deploy"
)
