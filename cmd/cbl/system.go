package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
)

func systemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Setups you run (TV, retro corner): their links, and bringing them up or down",
		Example: `  cbl system new "Retro corner"
  cbl system add-link "Retro corner" "grey ps2 av" --from "PS2:AV Multi Out" --to "CRT TV:AV in"
  cbl system up "Retro corner"
  cbl system down "Retro corner"`,
	}
	cmd.AddCommand(systemNewCmd(), systemAddLinkCmd(), systemUnlinkCmd(), systemUpCmd(), systemDownCmd(),
		systemLsCmd(), systemShowCmd(), systemRmCmd())
	return cmd
}

// state is everything the system commands read.
type state struct {
	c       *catalog.Catalog
	db      *library.DB
	inv     plan.Inventories
	systems []*library.System
	links   map[string][]*library.Link
	devices []*library.Device
}

func loadState() (*state, error) {
	c, err := loadCatalog()
	if err != nil {
		return nil, err
	}
	db, err := openLibrary()
	if err != nil {
		return nil, err
	}
	st := &state{c: c, db: db}
	if err := st.reload(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (st *state) reload() (err error) {
	if st.inv.Cables, err = st.db.LoadInventory(catalog.KindCable); err != nil {
		return err
	}
	if st.inv.Adapters, err = st.db.LoadInventory(catalog.KindAdapter); err != nil {
		return err
	}
	if st.systems, err = st.db.Systems(); err != nil {
		return err
	}
	if st.links, err = st.db.Links(); err != nil {
		return err
	}
	st.devices, err = st.db.Devices()
	return err
}

func (st *state) system(name string) (*library.System, error) {
	for _, s := range st.systems {
		if strings.EqualFold(s.Name, strings.TrimSpace(name)) {
			return s, nil
		}
	}
	return nil, fmt.Errorf("no system called %q; see `cbl system ls`", name)
}

func (st *state) partName(key string) string {
	if key == "" {
		return "plugs straight in"
	}
	if p, ok := st.c.Part(key); ok {
		return p.Name
	}
	return key
}

func (st *state) item(id string) *library.Item {
	if it, ok := st.inv.Cables.Items[id]; ok {
		return it
	}
	return st.inv.Adapters.Items[id]
}

func systemNewCmd() *cobra.Command {
	var notes string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a system",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLibrary()
			if err != nil {
				return err
			}
			defer db.Close()
			s, err := db.NewSystem(strings.Join(args, " "), notes)
			if err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]string{"id": s.ID, "name": s.Name})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %q. Add cables with `cbl system add-link %q <cable> --from <device>[:port] --to <device>[:port]`.\n", s.Name, s.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&notes, "notes", "", "free-form notes")
	return cmd
}

func systemAddLinkCmd() *cobra.Command {
	var from, to, variant string
	cmd := &cobra.Command{
		Use:   "add-link <system> <cable>",
		Short: "Add a cable or adapter to a system: one of yours (pinned) or any of a type",
		Long: `Add a cable or adapter to a system. Name one of your items (by id or
nickname) to pin it, or a type (by ends or name) to use any free one.
--from and --to take "device" or "device:port"; when the port is left out
and only one port fits, it is filled in.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			s, err := st.system(args[0])
			if err != nil {
				return err
			}
			l := library.Link{SystemID: s.ID}
			if l.PartKey, l.Preferred, err = st.resolvePart(cmd, args[1:], variant); err != nil {
				return err
			}
			if l.FromDevice, l.FromPort, err = st.endpoint(from, l.PartKey); err != nil {
				return err
			}
			if l.ToDevice, l.ToPort, err = st.endpoint(to, l.PartKey); err != nil {
				return err
			}
			added, err := st.db.AddLink(l)
			if err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, linkView(added))
			}
			out := cmd.OutOrStdout()
			what := st.partName(l.PartKey)
			if l.Preferred != "" {
				what += " #" + st.item(l.Preferred).ShortID()
			}
			fmt.Fprintf(out, "Linked %s: %s → %s\n", what, endpointText(l.FromDevice, l.FromPort), endpointText(l.ToDevice, l.ToPort))
			if len(plan.Short(st.c, append(st.links[s.ID], &l), st.inv)) > 0 {
				fmt.Fprintf(out, "You don't own enough of these for %q to come up; `cbl system up --wish` adds them to your wishlist.\n", s.Name)
			}
			if s.Active {
				fmt.Fprintf(out, "%q is up; take it down and up again to use the new link.\n", s.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", `source device, e.g. "PS2:AV Multi Out"`)
	cmd.Flags().StringVar(&to, "to", "", `destination device, e.g. "CRT TV:AV in"`)
	cmd.Flags().StringVar(&variant, "variant", "", "pick a variant when several types share the same ends (e.g. 8.5v)")
	return cmd
}

// resolvePart turns a cable argument into a part key, plus the item to pin
// when it names one of the user's items.
func (st *state) resolvePart(cmd *cobra.Command, args []string, variant string) (key, pinned string, err error) {
	ref := strings.Join(args, " ")
	for _, in := range []*library.Inventory{st.inv.Cables, st.inv.Adapters} {
		found := in.Lookup(ref)
		if len(found) == 0 {
			continue
		}
		for _, it := range found[1:] {
			if it.PartKey != found[0].PartKey {
				return "", "", fmt.Errorf("%q matches %d different items; use an id from `cbl ls`", ref, len(found))
			}
		}
		if len(found) == 1 {
			return found[0].PartKey, found[0].ID, nil
		}
		return found[0].PartKey, "", nil // identical items: any of them will do
	}
	for _, kind := range []catalog.Kind{catalog.KindCable, catalog.KindAdapter} {
		p, cands := st.c.Resolve(kind, args, variant)
		if p != nil {
			return p.Key, "", nil
		}
		if len(cands) > 1 {
			return "", "", unresolved(cmd, st.c, kind, args, variant, cands)
		}
	}
	return "", "", fmt.Errorf("no item or type called %q; see `cbl ls` or `cbl find`", ref)
}

// endpoint parses "device[:port]" and checks the part plugs into that port.
// With no port given, a single fitting port is filled in.
func (st *state) endpoint(spec, key string) (device, port string, err error) {
	if spec == "" {
		return "", "", nil
	}
	name, portName, _ := strings.Cut(spec, ":")
	found := library.FindDevices(st.devices, strings.TrimSpace(name))
	if len(found) != 1 {
		return "", "", fmt.Errorf("no single device called %q; see `cbl device ls`", name)
	}
	d := found[0]
	m, ok := st.c.Devices[d.Model]
	if !ok {
		if portName != "" {
			return "", "", fmt.Errorf("%q has no model yet, so its ports are unknown; see `cbl device add`", d.Nickname)
		}
		return d.Nickname, "", nil
	}
	var fitting []string
	for _, p := range m.Ports {
		for _, f := range st.c.Fits(p) {
			if f.Part.Key == key && !f.WrongVoltage {
				fitting = append(fitting, p.Label)
			}
		}
	}
	portName = strings.TrimSpace(portName)
	if portName == "" {
		switch len(fitting) {
		case 0:
			return "", "", fmt.Errorf("%s doesn't plug into any port on %q; see `cbl device %q`", st.partName(key), d.Nickname, d.Nickname)
		case 1:
			return d.Nickname, fitting[0], nil
		}
		return d.Nickname, "", nil // several fit; leave it open
	}
	for _, p := range m.Ports {
		if strings.EqualFold(p.Label, portName) {
			for _, f := range fitting {
				if f == p.Label {
					return d.Nickname, p.Label, nil
				}
			}
			return "", "", fmt.Errorf("%s doesn't fit %q's %s port; see `cbl device %q`", st.partName(key), d.Nickname, p.Label, d.Nickname)
		}
	}
	var labels []string
	for _, p := range m.Ports {
		labels = append(labels, p.Label)
	}
	return "", "", fmt.Errorf("%q has no port %q; its ports are: %s", d.Nickname, portName, strings.Join(labels, ", "))
}

func endpointText(device, port string) string {
	switch {
	case device == "":
		return "?"
	case port == "":
		return device
	}
	return device + " · " + port
}

func systemUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <system> <link number>",
		Short: "Remove a link (numbers are from `cbl system show`)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			s, err := st.system(args[0])
			if err != nil {
				return err
			}
			if s.Active {
				return fmt.Errorf("%q is up; take it down first", s.Name)
			}
			links := st.links[s.ID]
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 || n > len(links) {
				return fmt.Errorf("%q has links 1 to %d", s.Name, len(links))
			}
			if err := st.db.RemoveLink(links[n-1].ID); err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]string{"removed": links[n-1].ID})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed link %d (%s) from %q\n", n, st.partName(links[n-1].PartKey), s.Name)
			return nil
		},
	}
}

func systemUpCmd() *cobra.Command {
	var switchOver, wish bool
	cmd := &cobra.Command{
		Use:   "up <system>",
		Short: "Bring a system up: its cables become in use",
		Long: `Bring a system up. Each link uses its pinned item, or any free item of the
same type when that one is busy. If a type has no free item, nothing changes
and the systems holding them are named; then --switch takes those systems
down first, and --wish adds the missing items to your wishlist instead.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			out := textOut(cmd)
			s, err := st.system(strings.Join(args, " "))
			if err != nil {
				return err
			}
			if s.Active {
				if jsonOut {
					return emit(cmd, map[string]any{"system": s.Name, "up": true, "already": true})
				}
				fmt.Fprintf(out, "%q is already up.\n", s.Name)
				return nil
			}
			links := st.links[s.ID]
			if len(links) == 0 {
				return fmt.Errorf("%q has no links yet; add some with `cbl system add-link`", s.Name)
			}

			up, err := st.bringUp(s, switchOver)
			if err != nil {
				return err
			}
			for _, name := range up.TookDown {
				fmt.Fprintf(out, "Took %q down.\n", name)
			}
			// Owning too few at all: only buying fixes it.
			if len(up.Missing) > 0 {
				var wished []string
				for _, m := range up.Missing {
					fmt.Fprintf(out, "%q needs %d × %s; you own %d.\n", s.Name, m.Need, st.partName(m.PartKey), m.Owned)
					if wish {
						if err := st.db.AddWish(m.PartKey, m.Need-m.Owned, s.ID, ""); err != nil {
							return err
						}
						fmt.Fprintf(out, "  ★ Added %d × %s to your wishlist.\n", m.Need-m.Owned, st.partName(m.PartKey))
						wished = append(wished, m.PartKey)
					}
				}
				if !wish {
					fmt.Fprintf(out, "  cbl system up %q --wish   add what's missing to your wishlist\n", s.Name)
				}
				err := fmt.Errorf("%q can't come up until you have them", s.Name)
				if jsonOut {
					emit(cmd, map[string]any{"error": err.Error(), "missing": up.Missing, "wished": wished})
					return errReported
				}
				return err
			}
			if len(up.Short) > 0 || len(up.Busy) > 0 {
				return conflict(cmd, out, st, s, up.Short, up.Busy, wish)
			}
			picks, tookDown := up.Picks, up.TookDown
			type useView struct {
				Link      string `json:"link"`
				Item      string `json:"item"`
				Part      string `json:"part"`
				InsteadOf string `json:"instead_of,omitempty"`
				BusyIn    string `json:"busy_in,omitempty"`
			}
			uses := []useView{}
			fmt.Fprintf(out, "%q is up. In use now:\n", s.Name)
			for _, p := range picks {
				u := useView{Link: p.Link.ID, Item: p.Item.ID, Part: p.Link.PartKey}
				line := fmt.Sprintf("  ✓ %s #%s", st.partName(p.Link.PartKey), p.Item.ShortID())
				if p.Swapped {
					pinned := st.item(p.Link.Preferred)
					u.InsteadOf, u.BusyIn = pinned.ID, st.inv.Of(st.c, p.Link.PartKey).HeldBy[pinned.ID]
					line += fmt.Sprintf("  (instead of #%s, busy in %q)", pinned.ShortID(), u.BusyIn)
				}
				fmt.Fprintln(out, line)
				uses = append(uses, u)
			}
			if jsonOut {
				return emit(cmd, map[string]any{"system": s.Name, "up": true, "took_down": tookDown, "in_use": uses})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&switchOver, "switch", false, "take down the systems holding the cables this one needs")
	cmd.Flags().BoolVar(&wish, "wish", false, "add the cables you're missing to your wishlist")
	return cmd
}

// upAttempt is what happened when bringing a system up. Unless Picks is set,
// nothing changed (apart from systems taken down to make room).
type upAttempt struct {
	Missing  []plan.Missing   // you own too few of a type at all
	Short    []plan.Shortage  // items busy in other systems
	Busy     []plan.PortClash // device ports other systems fill
	TookDown []string         // systems taken down by switchOver
	Picks    []plan.Pick      // the items now in use
}

// bringUp brings s up if every link gets an item and every port is free.
// With switchOver, systems holding what it needs are taken down first.
func (st *state) bringUp(s *library.System, switchOver bool) (*upAttempt, error) {
	links := st.links[s.ID]
	up := &upAttempt{TookDown: []string{}}
	if up.Missing = plan.Short(st.c, links, st.inv); len(up.Missing) > 0 {
		return up, nil
	}
	ports := plan.NewPorts(st.c, st.devices)
	picks, short := plan.Allocate(st.c, links, st.inv)
	busy := plan.BusyPorts(ports, s, st.systems, st.links)
	if (len(short) > 0 || len(busy) > 0) && switchOver {
		holders := map[string]bool{}
		for _, sh := range short {
			for _, h := range sh.HeldBy {
				holders[h] = true
			}
		}
		for _, b := range busy {
			for _, h := range b.HeldBy {
				holders[h] = true
			}
		}
		for _, other := range st.systems {
			if holders[other.Name] {
				if err := st.db.TakeDown(other.ID); err != nil {
					return nil, err
				}
				up.TookDown = append(up.TookDown, other.Name)
			}
		}
		if err := st.reload(); err != nil {
			return nil, err
		}
		picks, short = plan.Allocate(st.c, links, st.inv)
		busy = plan.BusyPorts(ports, s, st.systems, st.links)
	}
	if len(short) > 0 || len(busy) > 0 {
		up.Short, up.Busy = short, busy
		return up, nil
	}
	assigned := map[string]string{}
	for _, p := range picks {
		assigned[p.Link.ID] = p.Item.ID
	}
	if err := st.db.BringUp(s.ID, assigned); err != nil {
		return nil, err
	}
	up.Picks = picks
	return up, nil
}

// conflict explains why a system can't come up and what to do about it.
func conflict(cmd *cobra.Command, w io.Writer, st *state, s *library.System, short []plan.Shortage, busy []plan.PortClash, wish bool) error {
	type shortView struct {
		Link   string   `json:"link"`
		Part   string   `json:"part"`
		Owned  int      `json:"owned"`
		HeldBy []string `json:"held_by"`
	}
	shorts := []shortView{}
	wished := []string{}
	holders := map[string]bool{}
	for _, b := range busy {
		fmt.Fprintf(w, "%s is taken by %s.\n", b.Port, quoteAll(b.HeldBy))
		for _, h := range b.HeldBy {
			holders[h] = true
		}
	}
	if len(short) == 0 {
		wish = false // a busy port can't be bought around
	}
	for _, sh := range short {
		fmt.Fprintf(w, "No free %s for %s → %s: you own %d, in use by %s.\n", st.partName(sh.Link.PartKey),
			endpointText(sh.Link.FromDevice, sh.Link.FromPort), endpointText(sh.Link.ToDevice, sh.Link.ToPort),
			sh.Owned, quoteAll(sh.HeldBy))
		shorts = append(shorts, shortView{sh.Link.ID, sh.Link.PartKey, sh.Owned, sh.HeldBy})
		for _, h := range sh.HeldBy {
			holders[h] = true
		}
		if wish {
			if err := st.db.AddWish(sh.Link.PartKey, 1, s.ID, "so it can be up alongside "+strings.Join(sh.HeldBy, ", ")); err != nil {
				return err
			}
			fmt.Fprintf(w, "  ★ Added 1 × %s to your wishlist.\n", st.partName(sh.Link.PartKey))
			wished = append(wished, sh.Link.PartKey)
		}
	}
	if !wish {
		var names []string
		for h := range holders {
			names = append(names, h)
		}
		sort.Strings(names)
		fmt.Fprintln(w, "Choose one:")
		fmt.Fprintf(w, "  cbl system up %q --switch   take %s down first\n", s.Name, quoteAll(names))
		if len(short) > 0 {
			fmt.Fprintf(w, "  cbl system up %q --wish     add another to your wishlist\n", s.Name)
			fmt.Fprintln(w, "  cbl add …                    if you have another one, log it and retry")
		}
	}
	err := fmt.Errorf("%q is still down", s.Name)
	if jsonOut {
		emit(cmd, map[string]any{"error": err.Error(), "shortages": shorts, "busy_ports": busy, "wished": wished})
		return errReported
	}
	return err
}

func quoteAll(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = strconv.Quote(n)
	}
	return strings.Join(q, ", ")
}

func systemDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down <system>",
		Short: "Disconnect a system: its cables are freed",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			s, err := st.system(strings.Join(args, " "))
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !s.Active {
				if jsonOut {
					return emit(cmd, map[string]any{"system": s.Name, "up": false, "freed": []string{}})
				}
				fmt.Fprintf(out, "%q is already down.\n", s.Name)
				return nil
			}
			if err := st.db.TakeDown(s.ID); err != nil {
				return err
			}
			freed := []string{}
			for _, l := range st.links[s.ID] {
				if l.Assigned != "" {
					freed = append(freed, l.Assigned)
				}
			}
			if jsonOut {
				return emit(cmd, map[string]any{"system": s.Name, "up": false, "freed": freed})
			}
			n := len(freed)
			fmt.Fprintf(out, "%q is down; %d cables and adapters are back.\n", s.Name, n)
			return nil
		},
	}
}

func systemLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List systems, whether they're up, and which can't be up together",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			out := cmd.OutOrStdout()
			if jsonOut {
				conflicts := plan.Conflicts(st.c, plan.NewPorts(st.c, st.devices), st.systems, st.links, st.inv)
				views := []map[string]any{}
				for _, s := range st.systems {
					views = append(views, systemView(st, s, conflicts[s.ID]))
				}
				return emit(cmd, views)
			}
			if len(st.systems) == 0 {
				fmt.Fprintln(out, "No systems yet. Create one with `cbl system new <name>`.")
				return nil
			}
			conflicts := plan.Conflicts(st.c, plan.NewPorts(st.c, st.devices), st.systems, st.links, st.inv)
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SYSTEM\tSTATUS\tLINKS\tNOTES")
			for _, s := range st.systems {
				status := "down"
				if s.Active {
					status = "UP"
				}
				var notes []string
				if len(conflicts[s.ID]) > 0 {
					var names []string
					for _, c := range conflicts[s.ID] {
						names = append(names, c.Other.Name)
					}
					notes = append(notes, "not with "+quoteAll(names))
				}
				if len(plan.Short(st.c, st.links[s.ID], st.inv)) > 0 {
					notes = append(notes, "missing cables")
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", s.Name, status, len(st.links[s.ID]), strings.Join(notes, "; "))
			}
			return tw.Flush()
		},
	}
}

func systemShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <system>",
		Short: "A system's links, the cables they use, and what it conflicts with",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			s, err := st.system(strings.Join(args, " "))
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				return emit(cmd, systemView(st, s, plan.Conflicts(st.c, plan.NewPorts(st.c, st.devices), st.systems, st.links, st.inv)[s.ID]))
			}
			status := "down"
			if s.Active {
				status = "UP"
			}
			fmt.Fprintf(out, "%s · %s\n", s.Name, status)
			if s.Notes != "" {
				fmt.Fprintln(out, s.Notes)
			}
			links := st.links[s.ID]
			if len(links) == 0 {
				fmt.Fprintln(out, "\nNo links yet.")
				return nil
			}
			// A down system shows what it would use if brought up now.
			preview := map[string]plan.Pick{}
			if !s.Active {
				picks, _ := plan.Allocate(st.c, links, st.inv)
				for _, p := range picks {
					preview[p.Link.ID] = p
				}
			}
			fmt.Fprintln(out)
			for i, l := range links {
				fmt.Fprintf(out, "%2d. %s → %s\n", i+1, endpointText(l.FromDevice, l.FromPort), endpointText(l.ToDevice, l.ToPort))
				line := "    " + st.partName(l.PartKey)
				switch p, ok := preview[l.ID]; {
				case l.Direct():
					// Nothing to own: the device's own lead.
				case s.Active && l.Assigned != "":
					line += fmt.Sprintf("  #%s in use", st.item(l.Assigned).ShortID())
				case ok:
					line += fmt.Sprintf("  would use #%s", p.Item.ShortID())
					if p.Swapped {
						line += " (pinned one is busy)"
					}
				default:
					inv := st.inv.Of(st.c, l.PartKey)
					var held []string
					for _, it := range inv.Owned[l.PartKey] {
						if h := inv.HeldBy[it.ID]; h != "" {
							held = append(held, h)
						}
					}
					if len(held) > 0 {
						line += "  none free (in use by " + quoteAll(held) + ")"
					} else {
						line += "  you don't own one"
					}
				}
				if l.Preferred != "" {
					line += "  · pinned #" + st.item(l.Preferred).ShortID()
				}
				fmt.Fprintln(out, line)
			}
			for _, c := range plan.Conflicts(st.c, plan.NewPorts(st.c, st.devices), st.systems, st.links, st.inv)[s.ID] {
				if c.Port != "" {
					fmt.Fprintf(out, "\nCan't be up with %q: both plug into %s (it has %d).\n", c.Other.Name, c.Port, c.Have)
				} else {
					fmt.Fprintf(out, "\nCan't be up with %q: both need %s (%d together, you own %d).\n", c.Other.Name, st.partName(c.PartKey), c.Need, c.Have)
				}
			}
			for _, m := range plan.Short(st.c, links, st.inv) {
				fmt.Fprintf(out, "\nMissing: needs %d × %s, you own %d.\n", m.Need, st.partName(m.PartKey), m.Owned)
			}
			return nil
		},
	}
}

type linkJSON struct {
	ID         string `json:"id"`
	Part       string `json:"part"`
	Preferred  string `json:"preferred,omitempty"`
	Assigned   string `json:"assigned,omitempty"`
	FromDevice string `json:"from_device,omitempty"`
	FromPort   string `json:"from_port,omitempty"`
	ToDevice   string `json:"to_device,omitempty"`
	ToPort     string `json:"to_port,omitempty"`
}

func linkView(l *library.Link) linkJSON {
	return linkJSON{l.ID, l.PartKey, l.Preferred, l.Assigned, l.FromDevice, l.FromPort, l.ToDevice, l.ToPort}
}

func systemView(st *state, s *library.System, conflicts []plan.Conflict) map[string]any {
	links := []linkJSON{}
	for _, l := range st.links[s.ID] {
		links = append(links, linkView(l))
	}
	type conflictJSON struct {
		With string `json:"with"`
		Part string `json:"part,omitempty"`
		Port string `json:"port,omitempty"`
		Need int    `json:"need"`
		Have int    `json:"have"`
	}
	cs := []conflictJSON{}
	for _, c := range conflicts {
		cs = append(cs, conflictJSON{c.Other.Name, c.PartKey, c.Port, c.Need, c.Have})
	}
	missing := plan.Short(st.c, st.links[s.ID], st.inv)
	if missing == nil {
		missing = []plan.Missing{}
	}
	return map[string]any{"id": s.ID, "name": s.Name, "up": s.Active, "notes": s.Notes, "links": links, "conflicts": cs, "missing": missing}
}

func systemRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <system>",
		Short: "Delete a system that is down (your cables are untouched)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			s, err := st.system(strings.Join(args, " "))
			if err != nil {
				return err
			}
			if err := st.db.RemoveSystem(s); err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]string{"removed": s.ID})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted %q\n", s.Name)
			return nil
		},
	}
}
