// Package catalogdata holds the bundled connector catalog: the YAML you edit,
// and catalog.bin, the snapshot compiled from it that the CLI loads.
package catalogdata

//go:generate go run ../cmd/catalogc -dir . -o catalog.bin

import "embed"

// Version identifies this catalog release. Bump it whenever a YAML file
// changes, then run `go generate ./...`.
const Version = "2026.10.5"

//go:embed *.yaml
var FS embed.FS

// Compiled is catalog.bin. A test fails if it is older than the YAML.
//
//go:embed catalog.bin
var Compiled []byte
