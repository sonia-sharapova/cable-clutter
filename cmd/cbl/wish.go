package main

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
)

func wishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wish",
		Short: "Your shopping list of cables and adapters",
		Example: `  cbl wish                         # the list
  cbl wish add aux rca --for "Retro corner"
  cbl wish done 1 --where "drawer 2"  # bought it: it joins your inventory`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return listWishes(cmd, false) },
	}
	var all bool
	ls := &cobra.Command{
		Use:   "ls",
		Short: "List your wishlist",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return listWishes(cmd, all) },
	}
	ls.Flags().BoolVar(&all, "all", false, "include items already bought")
	cmd.AddCommand(ls, wishAddCmd(), wishDoneCmd(), wishRmCmd())
	return cmd
}

func listWishes(cmd *cobra.Command, all bool) error {
	st, err := loadState()
	if err != nil {
		return err
	}
	defer st.db.Close()
	wishes, err := st.db.Wishes(all)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(wishes) == 0 {
		fmt.Fprintln(out, "Your wishlist is empty. `cbl connect … --wish` and `cbl system up … --wish` add to it.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tITEM\tQTY\tFOR\tYOU OWN\tNOTES")
	n := 0
	for _, w := range wishes {
		num := "✓"
		if !w.Done {
			n++
			num = strconv.Itoa(n)
		}
		owned := len(st.inv.Of(st.c, w.PartKey).Owned[w.PartKey])
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%d\t%s\n", num, st.partName(w.PartKey), w.Qty, w.System, owned, w.Notes)
	}
	return tw.Flush()
}

// openWish finds an open wish by its number in `cbl wish`.
func openWish(db *library.DB, arg string) (*library.Wish, error) {
	wishes, err := db.Wishes(false)
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > len(wishes) {
		return nil, fmt.Errorf("no wish %q; your wishlist has 1 to %d", arg, len(wishes))
	}
	return wishes[n-1], nil
}

func wishAddCmd() *cobra.Command {
	var qty int
	var variant, notes, system string
	cmd := &cobra.Command{
		Use:   "add <ends|name>",
		Short: "Add a cable or adapter type to your wishlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			var part *catalog.Part
			for _, kind := range []catalog.Kind{catalog.KindCable, catalog.KindAdapter} {
				p, cands := st.c.Resolve(kind, args, variant)
				if p != nil {
					part = p
					break
				}
				if len(cands) > 1 {
					return unresolved(cmd.ErrOrStderr(), st.c, kind, args, variant, cands)
				}
			}
			if part == nil {
				return fmt.Errorf("no cable or adapter type called %q; see `cbl find`", strings.Join(args, " "))
			}
			systemID := ""
			if system != "" {
				s, err := st.system(system)
				if err != nil {
					return err
				}
				systemID = s.ID
			}
			if err := st.db.AddWish(part.Key, qty, systemID, notes); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "★ Added %d × %s to your wishlist\n", qty, part.Name)
			return nil
		},
	}
	cmd.Flags().IntVar(&qty, "qty", 1, "how many")
	cmd.Flags().StringVar(&variant, "variant", "", "pick a variant when several types share the same ends")
	cmd.Flags().StringVar(&notes, "notes", "", "free-form notes")
	cmd.Flags().StringVar(&system, "for", "", "the system it's for")
	return cmd
}

func wishDoneCmd() *cobra.Command {
	var it library.Item
	cmd := &cobra.Command{
		Use:   "done <number>",
		Short: "Mark a wish bought: it joins your inventory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			w, err := openWish(st.db, args[0])
			if err != nil {
				return err
			}
			it.Kind = catalog.KindCable
			if p, ok := st.c.Part(w.PartKey); ok {
				it.Kind = p.Kind
			}
			added, err := st.db.MarkWishDone(w, it)
			if err != nil {
				return err
			}
			ids := make([]string, len(added))
			for i, a := range added {
				ids[i] = "#" + a.ShortID()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Bought %d × %s; now in your inventory as %s\n", w.Qty, st.partName(w.PartKey), strings.Join(ids, " "))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&it.Nickname, "name", "", "nickname")
	f.StringVar(&it.Color, "color", "", "colour or label")
	f.StringVar(&it.Location, "where", "", "where it's kept")
	f.StringVar(&it.Notes, "notes", "", "free-form notes")
	f.StringVar(&it.Dedicated, "for", "", "device it belongs to")
	return cmd
}

func wishRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <number>",
		Short: "Remove a wish",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			w, err := openWish(db, args[0])
			if err != nil {
				return err
			}
			if err := db.RemoveWish(w.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed wish %s\n", args[0])
			return nil
		},
	}
}
