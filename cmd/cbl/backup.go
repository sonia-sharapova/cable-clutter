package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

func exportCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write your whole library as JSON (a backup you can read and import)",
		Example: `  cbl export -o cables-backup.json
  cbl export > cables-backup.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			e, err := db.Export(catalogdata.Version)
			if err != nil {
				return err
			}
			if file == "" {
				return emit(cmd, e)
			}
			f, err := os.Create(file)
			if err != nil {
				return err
			}
			if err := writeJSON(f, e); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]any{"file": file, "rows": rowCounts(e)})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Exported %s to %s\n", summary(rowCounts(e)), file)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "output", "o", "", "file to write (default: stdout)")
	return cmd
}

func importCmd() *cobra.Command {
	var replace bool
	cmd := &cobra.Command{
		Use:   "import <file|->",
		Short: "Load a library from `cbl export` output",
		Long: `Load a library from an export. The library must be empty unless --replace
is given, which deletes everything in it first. Use - to read stdin.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var data []byte
			var err error
			if args[0] == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(args[0])
			}
			if err != nil {
				return err
			}
			e, err := library.ParseExport(data)
			if err != nil {
				return err
			}
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			counts, err := db.Import(e, replace)
			if err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]any{"rows": counts})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported %s\n", summary(counts))
			return nil
		},
	}
	cmd.Flags().BoolVar(&replace, "replace", false, "delete everything in your library first")
	return cmd
}

func rowCounts(e *library.Export) map[string]int {
	out := map[string]int{}
	for tbl, rows := range e.Tables {
		out[tbl] = len(rows)
	}
	return out
}

// summary reads "34 cables, 12 adapters, 16 devices, …" from row counts.
func summary(counts map[string]int) string {
	names := map[string]string{"cable_items": "cables", "adapter_items": "adapters", "devices": "devices",
		"systems": "systems", "links": "links", "wishlist": "wishes", "custom_catalog": "custom catalog entries"}
	var parts []string
	for tbl, n := range counts {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, names[tbl]))
		}
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "an empty library"
	}
	return strings.Join(parts, ", ")
}
