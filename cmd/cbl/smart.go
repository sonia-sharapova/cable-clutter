package main

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/graph"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/route"
	"github.com/sonia-sharapova/cable-clutter/internal/smart"
)

// ownedDevice finds one of the user's devices that has a model.
func (st *state) ownedDevice(ref string) (smart.Device, error) {
	found := library.FindDevices(st.devices, ref)
	if len(found) != 1 {
		return smart.Device{}, fmt.Errorf("no single device of yours called %q; see `cbl device ls`", ref)
	}
	m, ok := st.c.Devices[found[0].Model]
	if !ok {
		return smart.Device{}, fmt.Errorf("%q has no model yet; see `cbl device add`", found[0].Nickname)
	}
	return smart.Device{Nickname: found[0].Nickname, Model: m}, nil
}

// routeParts names a route's parts: "HDMI cable → HDMI to composite …".
func routeParts(r *route.Route) string {
	names := make([]string, len(r.Steps))
	for i, s := range r.Steps {
		names[i] = s.Part.Name
	}
	return strings.Join(names, " → ")
}

func reachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reach <device>",
		Short: "Everything a device can connect to right now, with what you own",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			d, err := st.ownedDevice(strings.Join(args, " "))
			if err != nil {
				return err
			}
			all := smart.Devices(st.c, st.devices)
			reach := smart.ReachFrom(st.c, d, all, st.inv)
			if jsonOut {
				type reachJSON struct {
					Device   string               `json:"device"`
					Outgoing map[string]routeJSON `json:"outgoing"`
					Incoming map[string]routeJSON `json:"incoming"`
				}
				out := []reachJSON{}
				for _, r := range reach {
					out = append(out, reachJSON{r.Other.Nickname, routesJSON(r.Outgoing), routesJSON(r.Incoming)})
				}
				return emit(cmd, out)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "What %q can connect to with what you own:\n", d.Nickname)
			reached := map[string]bool{}
			for _, r := range reach {
				reached[r.Other.Nickname] = true
				fmt.Fprintf(w, "\n%s\n", r.Other.Nickname)
				for _, dir := range []struct {
					arrow  string
					routes map[string]*route.Route
				}{{"→", r.Outgoing}, {"←", r.Incoming}} {
					for _, kind := range smart.Kinds {
						if rt, ok := dir.routes[kind]; ok {
							fmt.Fprintf(w, "  %s %-5s  %s\n", dir.arrow, kind, routeParts(rt))
						}
					}
				}
			}
			var none []string
			for _, o := range all {
				if o.Nickname != d.Nickname && !reached[o.Nickname] {
					none = append(none, o.Nickname)
				}
			}
			if len(reach) == 0 {
				fmt.Fprintln(w, "\nNothing yet.")
			}
			if len(none) > 0 {
				fmt.Fprintf(w, "\nNo route with what you own to: %s\n(`cbl connect %q <device>` shows what to buy.)\n", strings.Join(none, ", "), d.Nickname)
			}
			return nil
		},
	}
}

func planCmd() *cobra.Command {
	var wish bool
	var save string
	cmd := &cobra.Command{
		Use:   "plan <device> <device> [<device>...]",
		Short: "Wire a chain of devices (A → B → C) without using one cable twice",
		Long: `Plan the cables and adapters for a chain of devices, each hop from one
device's outputs to the next one's inputs. No physical item gets two jobs,
except one cable carrying several signals on the same hop. --save turns the
plan into a system; --wish adds what's missing to your wishlist.`,
		Example: `  cbl plan ps2 "crt tv" "te speaker"
  cbl plan ps2 "crt tv" --save "Retro corner"`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			var chain []smart.Device
			for _, a := range args {
				d, err := st.ownedDevice(a)
				if err != nil {
					return err
				}
				chain = append(chain, d)
			}
			hops := smart.Chain(st.c, chain, st.inv, route.Options{})

			w := textOut(cmd)
			var buy []*catalog.Part
			seen := map[string]bool{}
			for i, h := range hops {
				fmt.Fprintf(w, "%sHop %d: %s → %s\n", map[bool]string{true: "\n", false: ""}[i > 0], i+1, h.From.Nickname, h.To.Nickname)
				if len(h.Routes) == 0 {
					fmt.Fprintln(w, "\n  No way to connect these, even buying parts.")
					for _, d := range []smart.Device{h.From, h.To} {
						if !d.Model.Confirmed {
							fmt.Fprintf(w, "  %q's ports aren't confirmed (assumes %s); it may have ports that would.\n", d.Nickname, d.Model.Assumption)
						}
					}
					continue
				}
				for _, p := range writeRoutes(w, st, h.From.Nickname, h.To.Nickname, h.Routes) {
					if !seen[p.Key] {
						seen[p.Key] = true
						buy = append(buy, p)
					}
				}
			}
			names := make([]string, len(chain))
			for i, d := range chain {
				names[i] = d.Nickname
			}
			wished, err := buyList(w, st, buy, wish, strings.Join(names, " → "))
			if err != nil {
				return err
			}
			var saved *library.System
			if save != "" {
				if saved, err = st.savePlan(save, hops); err != nil {
					return err
				}
				fmt.Fprintf(w, "\nSaved as system %q; bring it up with `cbl system up %q`.\n", saved.Name, saved.Name)
			}
			if jsonOut {
				type hopJSON struct {
					From   string               `json:"from"`
					To     string               `json:"to"`
					Routes map[string]routeJSON `json:"routes"`
				}
				out := []hopJSON{}
				for _, h := range hops {
					out = append(out, hopJSON{h.From.Nickname, h.To.Nickname, routesJSON(h.Routes)})
				}
				result := map[string]any{"hops": out, "to_buy": keysOf(buy), "wished": wished}
				if saved != nil {
					result["saved"] = saved.ID
				}
				return emit(cmd, result)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wish, "wish", false, "add the parts you'd need to buy to your wishlist")
	cmd.Flags().StringVar(&save, "save", "", "save the plan as a new system with this name")
	return cmd
}

// savePlan turns a plan into a system: one link per step of each distinct
// route, pinned to the item the plan chose. The first and last step of a
// route name the device ports; steps between are adapter chains.
func (st *state) savePlan(name string, hops []smart.Hop) (*library.System, error) {
	s, err := st.db.NewSystem(name, "")
	if err != nil {
		return nil, err
	}
	for _, h := range hops {
		var done []*route.Route
		for _, kind := range smart.Kinds {
			r, ok := h.Routes[kind]
			if !ok || slices.ContainsFunc(done, r.SameSteps) {
				continue
			}
			done = append(done, r)
			for i, step := range r.Steps {
				l := library.Link{SystemID: s.ID, PartKey: step.Part.Key}
				if step.Item != nil {
					l.Preferred = step.Item.ID
				}
				if i == 0 {
					l.FromDevice, l.FromPort = h.From.Nickname, r.From.Label
				}
				if i == len(r.Steps)-1 {
					l.ToDevice, l.ToPort = h.To.Nickname, r.To.Label
				}
				if _, err := st.db.AddLink(l); err != nil {
					return nil, err
				}
			}
		}
	}
	return s, nil
}

func bestBuyCmd() *cobra.Command {
	var top int
	cmd := &cobra.Command{
		Use:   "best-buy",
		Short: "The purchases that would connect the most of your devices",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			unlocks := smart.BestBuy(st.c, smart.Devices(st.c, st.devices), st.inv)
			if len(unlocks) > top {
				unlocks = unlocks[:top]
			}
			if jsonOut {
				type unlockJSON struct {
					Part  partView     `json:"part"`
					Pairs []smart.Pair `json:"connections"`
				}
				out := []unlockJSON{}
				for _, u := range unlocks {
					out = append(out, unlockJSON{viewPart(u.Part), u.Pairs})
				}
				return emit(cmd, out)
			}
			w := cmd.OutOrStdout()
			if len(unlocks) == 0 {
				fmt.Fprintln(w, "No single purchase connects anything new: everything reachable with one more part is already reachable.")
				return nil
			}
			fmt.Fprintln(w, "One purchase each; ranked by new device-to-device connections:")
			for i, u := range unlocks {
				fmt.Fprintf(w, "\n%2d. %s — %d new\n", i+1, u.Part.Name, len(u.Pairs))
				for j, p := range u.Pairs {
					if j == 4 {
						fmt.Fprintf(w, "      … and %d more\n", len(u.Pairs)-4)
						break
					}
					fmt.Fprintf(w, "      %s → %s (%s)\n", p.From, p.To, p.Kind)
				}
			}
			fmt.Fprintln(w, "\nAdd one with `cbl wish add \"<name>\"`.")
			return nil
		},
	}
	cmd.Flags().IntVar(&top, "top", 10, "how many to list")
	return cmd
}

func spareCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "spare",
		Short: "Cables that fit none of your devices, and ones worth a second of",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			unused := smart.Unused(st.c, smart.Devices(st.c, st.devices), st.inv)
			spares := smart.WorthASpare(st.c, plan.NewPorts(st.c, st.devices), st.systems, st.links, st.inv)
			if jsonOut {
				type unusedJSON struct {
					Part  string   `json:"part"`
					Name  string   `json:"name"`
					Items []string `json:"items"`
				}
				type spareJSON struct {
					Part    string      `json:"part"`
					Name    string      `json:"name"`
					Systems [][2]string `json:"frees_systems"`
				}
				u, sp := []unusedJSON{}, []spareJSON{}
				for _, k := range sortedKeys(unused) {
					var ids []string
					for _, it := range unused[k] {
						ids = append(ids, it.ID)
					}
					u = append(u, unusedJSON{k, st.partName(k), ids})
				}
				for _, k := range sortedKeys(spares) {
					sp = append(sp, spareJSON{k, st.partName(k), spares[k]})
				}
				return emit(cmd, map[string]any{"fits_no_device": u, "worth_a_spare": sp})
			}
			w := cmd.OutOrStdout()
			printUnused(w, st, unused)
			fmt.Fprintln(w)
			if len(spares) == 0 {
				fmt.Fprintln(w, "No systems are held back by a missing second cable.")
				return nil
			}
			fmt.Fprintln(w, "Worth a second one (your systems compete for it):")
			for _, k := range sortedKeys(spares) {
				var pairs []string
				for _, p := range spares[k] {
					pairs = append(pairs, fmt.Sprintf("%q and %q", p[0], p[1]))
				}
				fmt.Fprintf(w, "  %s: lets %s be up together\n", st.partName(k), strings.Join(pairs, ", "))
			}
			return nil
		},
	}
}

func printUnused(w io.Writer, st *state, unused map[string][]*library.Item) {
	if len(unused) == 0 {
		fmt.Fprintln(w, "Every cable and adapter you own fits at least one of your devices.")
		return
	}
	n := 0
	for _, items := range unused {
		n += len(items)
	}
	fmt.Fprintf(w, "Fits none of your devices (%d items): give away, or add the device it's for:\n", n)
	keys := sortedKeys(unused)
	sort.SliceStable(keys, func(i, j int) bool { return st.partName(keys[i]) < st.partName(keys[j]) })
	for _, k := range keys {
		var ids []string
		for _, it := range unused[k] {
			id := "#" + it.ShortID()
			if it.Nickname != "" {
				id += " " + it.Nickname
			}
			ids = append(ids, id)
		}
		fmt.Fprintf(w, "  %s  %s\n", st.partName(k), strings.Join(ids, ", "))
	}
}

func graphCmd() *cobra.Command {
	var format, device string
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Your devices, connectors and cables as a graph (text, Mermaid or Graphviz)",
		Example: `  cbl graph
  cbl graph --format mermaid > gear.mmd
  cbl graph --format dot | dot -Tsvg > gear.svg
  cbl graph --device PS2`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			g := graph.Build(st.c, smart.Devices(st.c, st.devices), st.inv)
			if device != "" {
				d, err := st.ownedDevice(device)
				if err != nil {
					return err
				}
				g = g.Around("dev:" + d.Nickname)
			}
			w := cmd.OutOrStdout()
			if jsonOut {
				return emit(cmd, map[string]any{"nodes": g.Nodes, "edges": g.Edges, "islands": g.Islands()})
			}
			switch format {
			case "mermaid":
				fmt.Fprint(w, g.Mermaid())
			case "dot":
				fmt.Fprint(w, g.DOT())
			case "text":
				printGraph(w, g)
			default:
				return fmt.Errorf("unknown format %q: want text, mermaid or dot", format)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "text, mermaid or dot")
	cmd.Flags().StringVar(&device, "device", "", "only what's connected to this device")
	return cmd
}

func printGraph(w io.Writer, g *graph.Graph) {
	label := map[string]string{}
	for _, n := range g.Nodes {
		label[n.ID] = n.Label
	}
	fmt.Fprintln(w, "Devices and the connectors they have:")
	for _, n := range g.Nodes {
		if !n.Device {
			continue
		}
		var conns []string
		for _, e := range g.Edges {
			if e.Kind == graph.Port && e.From == n.ID {
				conns = append(conns, label[e.To])
			}
		}
		fmt.Fprintf(w, "  %s: %s\n", n.Label, strings.Join(conns, ", "))
	}
	fmt.Fprintln(w, "\nWhat you own joins (connector — connector):")
	for _, e := range g.Edges {
		kind := map[graph.EdgeKind]string{graph.Cable: "cable", graph.Adapter: "adapter", graph.Fits: "plugs fit"}[e.Kind]
		if kind == "" {
			continue
		}
		line := fmt.Sprintf("  %s — %s  (%s", label[e.From], label[e.To], kind)
		if e.Part != "" {
			line += ": " + e.Part
		}
		if e.Label != "" && e.Kind != graph.Fits {
			line += " " + e.Label
		}
		fmt.Fprintln(w, line+")")
	}
	if islands := g.Islands(); len(islands) > 0 {
		fmt.Fprintf(w, "\nNothing you own plugs into: %s\n", strings.Join(islands, ", "))
	}
}
