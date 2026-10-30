// Command cbl is the Cable Clutter CLI.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
)

var timing bool

// version is set at release time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := newRoot().Execute(); err != nil {
		reportError(os.Stdout, os.Stderr, err)
		os.Exit(1)
	}
}

// reportError prints a failed command's error: as JSON on stdout under
// --json, unless the command already reported it; otherwise on stderr.
func reportError(stdout, stderr io.Writer, err error) {
	switch {
	case errors.Is(err, errReported):
	case jsonOut:
		writeJSON(stdout, map[string]string{"error": err.Error()})
	default:
		fmt.Fprintln(stderr, "cbl:", err)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "cbl",
		Short:         "Cable Clutter: know your cables, ports and what you're missing",
		Version:       version + " (catalog " + catalogdata.Version + ")",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&timing, "timing", false, "print load and query times to stderr")
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "print JSON instead of text")
	root.AddCommand(
		findCmd(), showCmd(), deviceCmd(), systemCmd(), connectCmd(), planCmd(), reachCmd(), bestBuyCmd(), spareCmd(), graphCmd(), wishCmd(), exportCmd(), importCmd(), catalogCmd(), doctorCmd(),
		addCmd(catalog.KindCable), lsCmd(catalog.KindCable), rmCmd(catalog.KindCable), adapterCmd(),
	)
	return root
}

func loadCatalog() (*catalog.Catalog, error) {
	start := time.Now()
	c, err := catalog.LoadCompiled(catalogdata.Compiled, catalogdata.Version)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	report("catalog load", start)
	return c, nil
}

func report(what string, start time.Time) {
	if timing {
		fmt.Fprintf(os.Stderr, "[timing] %s: %s\n", what, time.Since(start))
	}
}
