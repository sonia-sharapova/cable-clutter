package main

import (
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"cable-clutter/internal/library"
)

func catalogCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "catalog [connectors|cables|adapters|signals]",
		Short:     "List what the bundled catalog knows about",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"connectors", "cables", "adapters", "signals"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := loadCatalog()
			if err != nil {
				return err
			}
			what := "connectors"
			if len(args) == 1 {
				what = args[0]
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			defer tw.Flush()
			switch what {
			case "connectors":
				for _, id := range sortedKeys(c.Connectors) {
					cn := c.Connectors[id]
					fmt.Fprintf(tw, "%s\t%s\t%s\n", cn.Family, cn.ID, cn.Name)
				}
			case "cables", "adapters":
				m := c.Cables
				if what == "adapters" {
					m = c.Adapters
				}
				for _, k := range sortedKeys(m) {
					p := m[k]
					fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Name, p.Key, partFlags(p))
				}
			case "signals":
				for _, id := range sortedKeys(c.Signals) {
					s := c.Signals[id]
					fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Kind, s.ID, s.Name)
				}
			default:
				return fmt.Errorf("unknown list %q: want connectors, cables, adapters or signals", what)
			}
			return nil
		},
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the catalog and library database and show where data lives",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			c, err := loadCatalog()
			if err != nil {
				return err
			}
			loadTime := time.Since(start)
			fmt.Fprintf(cmd.OutOrStdout(), "catalog   v%s: %d connectors, %d cables, %d adapters, %d signals (loaded in %s)\n",
				c.Version, len(c.Connectors), len(c.Cables), len(c.Adapters), len(c.Signals), loadTime.Round(time.Microsecond))

			path, err := library.DefaultPath()
			if err != nil {
				return err
			}
			db, err := library.Open(path)
			if err != nil {
				return err
			}
			defer db.Close()
			v, err := db.SchemaVersion()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "library   %s (schema v%d of %d)\n", db.Path, v, library.LatestVersion())
			return nil
		},
	}
}
