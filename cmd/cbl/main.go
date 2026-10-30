// Command cbl is the Cable Clutter CLI.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	catalogdata "cable-clutter/catalog"
	"cable-clutter/internal/catalog"
)

var timing bool

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "cbl:", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "cbl",
		Short:         "Cable Clutter: know your cables, ports and what you're missing",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&timing, "timing", false, "print load and query times to stderr")
	root.AddCommand(
		findCmd(), showCmd(), catalogCmd(), doctorCmd(),
		addCmd(catalog.KindCable), lsCmd(catalog.KindCable), rmCmd(catalog.KindCable), adapterCmd(),
	)
	return root
}

func loadCatalog() (*catalog.Catalog, error) {
	start := time.Now()
	c, err := catalog.Load(catalogdata.FS, catalogdata.Version)
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
