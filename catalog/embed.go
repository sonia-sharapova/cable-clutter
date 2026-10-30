// Package catalogdata holds the bundled connector catalog as YAML, compiled
// into the binary so the CLI needs no data files at runtime.
package catalogdata

import "embed"

// Version identifies this catalog release. Bump it whenever a YAML file changes.
const Version = "2026.10.2"

//go:embed *.yaml
var FS embed.FS
