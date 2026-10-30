// Command catalogc compiles the YAML catalog into the binary snapshot the CLI
// embeds. Run it through `go generate ./...` after editing catalog/*.yaml.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
)

func main() {
	dir := flag.String("dir", ".", "directory holding the catalog YAML")
	out := flag.String("o", "catalog.bin", "snapshot to write")
	flag.Parse()
	data, err := catalog.Compile(os.DirFS(*dir))
	if err != nil {
		fmt.Fprintln(os.Stderr, "catalogc:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "catalogc:", err)
		os.Exit(1)
	}
}
