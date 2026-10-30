package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/route"
	"github.com/sonia-sharapova/cable-clutter/internal/smart"
)

func connectCmd() *cobra.Command {
	var wish, ownedOnly bool
	var maxSteps int
	cmd := &cobra.Command{
		Use:   "connect <device> <device>",
		Short: "Find how to hook one device up to another with what you own",
		Long: `Find the cheapest chain of cables and adapters from one device's outputs to
another's inputs, for each kind of signal they share (video, audio, data).
Items you own and is free are used first, then ones in use elsewhere, and
only then does it suggest buying. --wish adds whatever's missing to your
wishlist.`,
		Example: `  cbl connect ps2 "crt tv"
  cbl connect "macbook pro" "dell monitor" --wish`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			a, aName, _, err := resolveModel(st.c, st.devices, args[0])
			if err != nil {
				return err
			}
			b, bName, _, err := resolveModel(st.c, st.devices, args[1])
			if err != nil {
				return err
			}
			if aName == "" {
				aName = a.Name
			}
			if bName == "" {
				bName = b.Name
			}
			opt := route.Options{MaxSteps: maxSteps, OwnedOnly: ownedOnly}
			routes := route.Connect(st.c, a, b, st.inv, opt)
			if len(routes) == 0 { // maybe it only works the other way round
				if rev := route.Connect(st.c, b, a, st.inv, opt); len(rev) > 0 {
					routes, aName, bName = rev, bName, aName
				}
			}
			out := textOut(cmd)
			if len(routes) == 0 {
				return fmt.Errorf("no way to connect %s and %s within %d cables and adapters%s",
					aName, bName, max(maxSteps, 1), map[bool]string{true: " you own", false: ""}[ownedOnly])
			}
			fmt.Fprintf(out, "%s → %s\n", aName, bName)
			buy := writeRoutes(out, st, aName, bName, routes)
			wished, err := buyList(out, st, buy, wish, aName+" → "+bName)
			if err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]any{"from": aName, "to": bName, "routes": routesJSON(routes), "to_buy": keysOf(buy), "wished": wished})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wish, "wish", false, "add the parts you'd need to buy to your wishlist")
	cmd.Flags().BoolVar(&ownedOnly, "owned", false, "only use what you own")
	cmd.Flags().IntVar(&maxSteps, "max", route.DefaultMax, "most cables and adapters in one chain")
	return cmd
}

type stepJSON struct {
	Part   partView `json:"part"`
	In     string   `json:"in"`
	Out    string   `json:"out"`
	Item   string   `json:"item,omitempty"`
	BusyIn string   `json:"busy_in,omitempty"`
	Buy    bool     `json:"buy,omitempty"`
}

type routeJSON struct {
	Signal string     `json:"signal"`
	From   string     `json:"from_port"`
	To     string     `json:"to_port"`
	Cost   int        `json:"cost"`
	Steps  []stepJSON `json:"steps"`
}

func routesJSON(routes map[string]*route.Route) map[string]routeJSON {
	out := map[string]routeJSON{}
	for kind, r := range routes {
		rj := routeJSON{Signal: r.Signal, From: r.From.Label, To: r.To.Label, Cost: r.Cost}
		for _, s := range r.Steps {
			sj := stepJSON{Part: viewPart(s.Part), In: s.In.String(), Out: s.Out.String(), BusyIn: s.Busy, Buy: s.Item == nil}
			if s.Item != nil {
				sj.Item = s.Item.ID
			}
			rj.Steps = append(rj.Steps, sj)
		}
		out[kind] = rj
	}
	return out
}

// writeRoutes prints the route for each signal kind between two devices and
// returns the parts they need you to buy, each once.
func writeRoutes(w io.Writer, st *state, aName, bName string, routes map[string]*route.Route) []*catalog.Part {
	var shown []*route.Route
	shownKind := map[*route.Route]string{}
	var buy []*catalog.Part
	seen := map[string]bool{}
	for _, kind := range smart.Kinds {
		r, ok := routes[kind]
		if !ok {
			continue
		}
		fmt.Fprintf(w, "\n%s · %s", strings.ToUpper(kind), st.c.Signals[r.Signal].Name)
		same := false
		for _, prev := range shown {
			if prev.SameSteps(r) {
				fmt.Fprintf(w, ": same cables as %s\n", shownKind[prev])
				same = true
				break
			}
		}
		if same {
			continue
		}
		fmt.Fprintln(w)
		shown = append(shown, r)
		shownKind[r] = kind

		fmt.Fprintf(w, "  %s · %s\n", aName, r.From.Label)
		for _, s := range r.Steps {
			line := "    → " + s.Part.Name
			switch {
			case s.Item == nil:
				line = "    ★ NEED: " + s.Part.Name + "  (" + s.Part.Key + ")"
				if !seen[s.Part.Key] {
					seen[s.Part.Key] = true
					buy = append(buy, s.Part)
				}
			case s.Busy != "":
				line += fmt.Sprintf("  #%s in use by %q", s.Item.ShortID(), s.Busy)
			default:
				line += fmt.Sprintf("  #%s free", s.Item.ShortID())
			}
			fmt.Fprintln(w, line)
		}
		fmt.Fprintf(w, "  %s · %s\n", bName, r.To.Label)
	}
	return buy
}

// buyList prints what to buy and, with wish, adds each part to the wishlist
// unless it's already there. It returns the keys it added.
func buyList(w io.Writer, st *state, buy []*catalog.Part, wish bool, note string) ([]string, error) {
	wished := []string{}
	if len(buy) == 0 {
		fmt.Fprintln(w, "\nEverything needed is in your inventory.")
		return wished, nil
	}
	fmt.Fprintln(w)
	for _, p := range buy {
		if !wish {
			fmt.Fprintf(w, "To buy: %s\n", p.Name)
			continue
		}
		if wanted, err := st.db.Wanted(p.Key); err != nil {
			return nil, err
		} else if wanted {
			fmt.Fprintf(w, "Already on your wishlist: %s\n", p.Name)
			continue
		}
		if err := st.db.AddWish(p.Key, 1, "", note); err != nil {
			return nil, err
		}
		fmt.Fprintf(w, "★ Added to your wishlist: %s\n", p.Name)
		wished = append(wished, p.Key)
	}
	if !wish {
		fmt.Fprintln(w, "Add these to your wishlist with --wish.")
	}
	return wished, nil
}

func keysOf(parts []*catalog.Part) []string {
	out := []string{}
	for _, p := range parts {
		out = append(out, p.Key)
	}
	return out
}
