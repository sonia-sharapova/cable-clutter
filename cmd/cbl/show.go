package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/outline"
)

func showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id|name|ends>",
		Short: "Draw a cable or adapter: its ends side by side, with status",
		Example: `  cbl show 826e39             # one of your items, by id
  cbl show "grey ps2 av"      # or by nickname
  cbl show usb-a usb-c        # any catalog type, by ends
  cbl show "usb-c hub with card reader"`,
		Args: cobra.MinimumNArgs(1),
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
			out := cmd.OutOrStdout()

			// Your own items first, by id or nickname.
			ref := strings.Join(args, " ")
			for _, kind := range []catalog.Kind{catalog.KindCable, catalog.KindAdapter} {
				inv, err := db.LoadInventory(kind)
				if err != nil {
					return err
				}
				found := inv.Lookup(ref)
				if len(found) == 0 {
					continue
				}
				// Several items are fine if they're the same type (e.g. two
				// identical adaptors): draw once, list each.
				for _, it := range found[1:] {
					if it.PartKey != found[0].PartKey {
						return fmt.Errorf("%q matches %d different %ss; use an id from `ls`", ref, len(found), kind)
					}
				}
				p, ok := c.Part(found[0].PartKey)
				if !ok {
					return fmt.Errorf("#%s is a %s the catalog no longer knows", found[0].ShortID(), found[0].PartKey)
				}
				if jsonOut {
					return emit(cmd, map[string]any{"part": viewPart(p), "items": viewItems(c, found, inv), "outline": outline.Render(c, p, "")})
				}
				showItems(out, c, p, found, inv)
				return nil
			}

			// Otherwise a catalog type.
			for _, kind := range []catalog.Kind{catalog.KindCable, catalog.KindAdapter} {
				p, cands := c.Resolve(kind, args, "")
				if p == nil && len(cands) > 1 {
					return unresolved(cmd, c, kind, args, "", cands)
				}
				if p != nil {
					inv, err := db.LoadInventory(kind)
					if err != nil {
						return err
					}
					if jsonOut {
						free := 0
						for _, it := range inv.Owned[p.Key] {
							if inv.Status[it.ID] == library.Free {
								free++
							}
						}
						return emit(cmd, map[string]any{"part": viewPart(p), "owned": len(inv.Owned[p.Key]), "free": free, "outline": outline.Render(c, p, "")})
					}
					showType(out, c, p, inv)
					return nil
				}
			}
			return fmt.Errorf("nothing called %q in your inventory or the catalog; try `cbl find`", ref)
		},
	}
}

func showItems(w io.Writer, c *catalog.Catalog, p *catalog.Part, items []*library.Item, inv *library.Inventory) {
	colors := map[library.Status]string{library.Free: outline.Green, library.Reserved: outline.Amber, library.InUse: outline.Red}
	color := ""
	if useColor(w) {
		color = colors[inv.Status[items[0].ID]]
		for _, it := range items[1:] {
			if inv.Status[it.ID] != inv.Status[items[0].ID] {
				color = "" // mixed statuses: leave the drawing plain
			}
		}
	}
	fmt.Fprintln(w)
	fmt.Fprint(w, indent(outline.Render(c, p, color)))
	fmt.Fprintln(w)

	for _, it := range items {
		var head []string
		if it.Nickname != "" {
			head = append(head, it.Nickname)
		}
		head = append(head, "#"+it.ShortID())
		for _, x := range []string{it.Color, it.Location} {
			if x != "" {
				head = append(head, x)
			}
		}
		if it.Dedicated != "" {
			head = append(head, "for "+it.Dedicated)
		}
		s := inv.Status[it.ID]
		status := strings.ToUpper(s.String())
		if useColor(w) {
			status = colors[s] + status + outline.Reset
		}
		fmt.Fprintf(w, "  %s · %s\n", strings.Join(head, " · "), status)
	}
	describe(w, c, p)
	for _, it := range items {
		if it.Notes != "" {
			fmt.Fprintf(w, "  Notes (#%s): %s\n", it.ShortID(), it.Notes)
		}
	}
}

func showType(w io.Writer, c *catalog.Catalog, p *catalog.Part, inv *library.Inventory) {
	fmt.Fprintln(w)
	fmt.Fprint(w, indent(outline.Render(c, p, "")))
	fmt.Fprintln(w)
	describe(w, c, p)
	owned, free := inv.Owned[p.Key], 0
	for _, it := range owned {
		if inv.Status[it.ID] == library.Free {
			free++
		}
	}
	if len(owned) == 0 {
		fmt.Fprintln(w, "  You don't own one.")
	} else {
		fmt.Fprintf(w, "  You own %d (%d free).\n", len(owned), free)
	}
}

// describe prints the catalog facts under a drawing.
func describe(w io.Writer, c *catalog.Catalog, p *catalog.Part) {
	line := fmt.Sprintf("  %s  %s", p.Name, p.Key)
	if f := partFlags(p); f != "" {
		line += "  " + f
	}
	fmt.Fprintln(w, line)
	names := make([]string, len(p.Signals))
	for i, s := range p.Signals {
		names[i] = c.Signals[s].Name
	}
	fmt.Fprintf(w, "  Carries: %s\n", strings.Join(names, "; "))
	if p.Note != "" {
		fmt.Fprintf(w, "  Note: %s\n", p.Note)
	}
}

func indent(s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "")
}

// useColor is true when w is a terminal and NO_COLOR is unset.
func useColor(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
