package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
)

func openLibrary() (*library.DB, error) {
	path, err := library.DefaultPath()
	if err != nil {
		return nil, err
	}
	return library.Open(path)
}

// noun is "cable" or "adapter", for messages.
func noun(kind catalog.Kind) string { return string(kind) }

func addCmd(kind catalog.Kind) *cobra.Command {
	var it library.Item
	var qty int
	var variant string
	cmd := &cobra.Command{
		Use:   "add <end> <end> [<end>...] | add <name>",
		Short: fmt.Sprintf("Log %s you own", noun(kind)+"s"),
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := loadCatalog()
			if err != nil {
				return err
			}
			p, candidates := c.Resolve(kind, args, variant)
			if p == nil {
				return unresolved(cmd.ErrOrStderr(), c, kind, args, variant, candidates)
			}
			if qty < 1 {
				return fmt.Errorf("--qty must be at least 1")
			}
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			it.Kind, it.PartKey = kind, p.Key
			added, err := db.AddItems(it, qty)
			if err != nil {
				return err
			}
			ids := make([]string, len(added))
			for i, a := range added {
				ids[i] = "#" + a.ShortID()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %d × %s  (%s)  %s\n", qty, p.Name, p.Key, strings.Join(ids, " "))
			return nil
		},
	}
	if kind == catalog.KindCable {
		cmd.Example = `  cbl add usb-a usb-c --qty 4
  cbl add ps2-avmulti rca --for PS2 --where "drawer 2"
  cbl add aux rca          # 3.5 mm to red/white RCA
  cbl add "aux cable"      # by name`
	} else {
		cmd.Example = `  cbl adapter add "usb c hub with card reader"
  cbl adapter add nema-1-15:f cee-7-16:m --qty 3`
	}
	f := cmd.Flags()
	f.IntVar(&qty, "qty", 1, "how many identical items to add")
	f.StringVar(&variant, "variant", "", "pick a variant when several types share the same ends (e.g. 9v)")
	f.StringVar(&it.Nickname, "name", "", "nickname, e.g. \"grey ps2 av\"")
	f.StringVar(&it.Color, "color", "", "colour or label")
	f.StringVar(&it.Location, "where", "", "where it's kept")
	f.StringVar(&it.Notes, "notes", "", "free-form notes")
	f.StringVar(&it.Dedicated, "for", "", "device it belongs to (preferred for that device)")
	return cmd
}

// unresolved explains why no single catalog type matched and what to try.
func unresolved(w io.Writer, c *catalog.Catalog, kind catalog.Kind, args []string, variant string, candidates []*catalog.Part) error {
	what := strings.Join(args, " ")
	if len(candidates) == 0 {
		other := catalog.KindAdapter
		if kind == catalog.KindAdapter {
			other = catalog.KindCable
		}
		if p, alt := c.Resolve(other, args, variant); p != nil || len(alt) > 0 {
			hint := "cbl add"
			if other == catalog.KindAdapter {
				hint = "cbl adapter add"
			}
			return fmt.Errorf("no %s type matches %q, but there is an %s: try `%s %s`", kind, what, other, hint, what)
		}
		return fmt.Errorf("no %s type matches %q; see `cbl catalog %ss` or `cbl find`", kind, what, kind)
	}
	fmt.Fprintf(w, "%q matches %d %s types:\n", what, len(candidates), kind)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for i, p := range candidates {
		if i == 10 {
			fmt.Fprintf(tw, "  … and %d more\n", len(candidates)-10)
			break
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", p.Name, p.Key, partFlags(p))
	}
	tw.Flush()
	return fmt.Errorf("be more specific: add a gender (usb-c:f), a variant (--variant 9v), or pass the key in quotes")
}

func lsCmd(kind catalog.Kind) *cobra.Command {
	var free, reserved, inUse, summary bool
	var connector string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: fmt.Sprintf("List your %ss and whether they're free", noun(kind)),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := loadCatalog()
			if err != nil {
				return err
			}
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			inv, err := db.LoadInventory(kind)
			if err != nil {
				return err
			}

			var withConn map[string]bool
			if connector != "" {
				id, ok := c.ResolveConnector(connector)
				if !ok {
					return fmt.Errorf("unknown connector %q", connector)
				}
				withConn = map[string]bool{}
				for _, p := range c.ByConnector[id] {
					withConn[p.Key] = true
				}
			}
			want := func(it *library.Item) bool {
				if withConn != nil && !withConn[it.PartKey] {
					return false
				}
				if !free && !reserved && !inUse {
					return true
				}
				s := inv.Status[it.ID]
				return (free && s == library.Free) || (reserved && s == library.Reserved) || (inUse && s == library.InUse)
			}
			var items []*library.Item
			for _, it := range inv.Order {
				if want(it) {
					items = append(items, it)
				}
			}
			name := func(it *library.Item) string {
				if p, ok := c.Part(it.PartKey); ok {
					return p.Name
				}
				return it.PartKey // type removed from the catalog
			}
			sort.SliceStable(items, func(i, j int) bool { return name(items[i]) < name(items[j]) })

			out := cmd.OutOrStdout()
			if len(items) == 0 {
				fmt.Fprintf(out, "No %ss match. Add one with `%s`.\n", kind, addHint(kind))
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			if summary {
				fmt.Fprintln(tw, "TYPE\tKEY\tOWNED\tFREE")
				var keys []string
				counts, frees := map[string]int{}, map[string]int{}
				for _, it := range items {
					if counts[it.PartKey] == 0 {
						keys = append(keys, it.PartKey)
					}
					counts[it.PartKey]++
					if inv.Status[it.ID] == library.Free {
						frees[it.PartKey]++
					}
				}
				for _, k := range keys {
					fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", name(inv.Owned[k][0]), k, counts[k], frees[k])
				}
			} else {
				fmt.Fprintln(tw, "ID\tSTATUS\tTYPE\tNAME\tWHERE\tFOR")
				for _, it := range items {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", it.ShortID(), inv.Status[it.ID], name(it), it.Nickname, it.Location, it.Dedicated)
				}
			}
			tw.Flush()
			byStatus := map[library.Status]int{}
			for _, it := range items {
				byStatus[inv.Status[it.ID]]++
			}
			fmt.Fprintf(out, "\n%d %ss: %d free, %d reserved, %d in use\n", len(items), kind,
				byStatus[library.Free], byStatus[library.Reserved], byStatus[library.InUse])
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&free, "free", false, "only free items")
	f.BoolVar(&reserved, "reserved", false, "only items reserved by an inactive system")
	f.BoolVar(&inUse, "in-use", false, "only items in an active system")
	f.BoolVar(&summary, "summary", false, "one line per type with counts")
	f.StringVar(&connector, "type", "", "only items with this connector, e.g. usb-c or aux")
	return cmd
}

func addHint(kind catalog.Kind) string {
	if kind == catalog.KindAdapter {
		return "cbl adapter add"
	}
	return "cbl add"
}

func rmCmd(kind catalog.Kind) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id|name>",
		Short: fmt.Sprintf("Remove a %s from your inventory", noun(kind)),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			inv, err := db.LoadInventory(kind)
			if err != nil {
				return err
			}
			found := inv.Lookup(args[0])
			switch {
			case len(found) == 0:
				return fmt.Errorf("no %s %q; see `%s`", kind, args[0], strings.Replace(addHint(kind), "add", "ls", 1))
			case len(found) > 1:
				return fmt.Errorf("%q matches %d %ss; use the id from `ls`", args[0], len(found), kind)
			}
			it := found[0]
			if s := inv.Status[it.ID]; s != library.Free {
				return fmt.Errorf("#%s is %s; take its system down first", it.ShortID(), s)
			}
			if err := db.RemoveItem(kind, it.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed #%s\n", it.ShortID())
			return nil
		},
	}
}

func adapterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapter",
		Short: "Your adapter inventory (plug converters, couplers, hubs, chargers)",
	}
	cmd.AddCommand(addCmd(catalog.KindAdapter), lsCmd(catalog.KindAdapter), rmCmd(catalog.KindAdapter))
	return cmd
}
