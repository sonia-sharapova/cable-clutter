package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"cable-clutter/internal/catalog"
	"cable-clutter/internal/route"
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
			out := cmd.OutOrStdout()
			if len(routes) == 0 {
				return fmt.Errorf("no way to connect %s and %s within %d cables and adapters%s",
					aName, bName, max(maxSteps, 1), map[bool]string{true: " you own", false: ""}[ownedOnly])
			}
			return printRoutes(out, st, aName, bName, routes, wish)
		},
	}
	cmd.Flags().BoolVar(&wish, "wish", false, "add the parts you'd need to buy to your wishlist")
	cmd.Flags().BoolVar(&ownedOnly, "owned", false, "only use what you own")
	cmd.Flags().IntVar(&maxSteps, "max", route.DefaultMax, "most cables and adapters in one chain")
	return cmd
}

var kindOrder = []string{"video", "audio", "data"}

func printRoutes(w io.Writer, st *state, aName, bName string, routes map[string]*route.Route, wish bool) error {
	fmt.Fprintf(w, "%s → %s\n", aName, bName)
	var shown []*route.Route
	shownKind := map[*route.Route]string{}
	buy := map[string]*catalog.Part{}
	var buyOrder []string
	for _, kind := range kindOrder {
		r, ok := routes[kind]
		if !ok {
			continue
		}
		sig := st.c.Signals[r.Signal].Name
		fmt.Fprintf(w, "\n%s · %s", strings.ToUpper(kind), sig)
		same := false
		for _, prev := range shown {
			if prev.SameSteps(r) {
				fmt.Fprintf(w, ": same cables as %s\n", strings.ToLower(shownKind[prev]))
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
				if _, dup := buy[s.Part.Key]; !dup {
					buy[s.Part.Key] = s.Part
					buyOrder = append(buyOrder, s.Part.Key)
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

	if len(buyOrder) == 0 {
		fmt.Fprintln(w, "\nEverything needed is in your inventory.")
		return nil
	}
	fmt.Fprintln(w)
	for _, key := range buyOrder {
		p := buy[key]
		if !wish {
			fmt.Fprintf(w, "To buy: %s\n", p.Name)
			continue
		}
		if wanted, err := st.db.Wanted(key); err != nil {
			return err
		} else if wanted {
			fmt.Fprintf(w, "Already on your wishlist: %s\n", p.Name)
			continue
		}
		if err := st.db.AddWish(key, 1, "", fmt.Sprintf("%s → %s", aName, bName)); err != nil {
			return err
		}
		fmt.Fprintf(w, "★ Added to your wishlist: %s\n", p.Name)
	}
	if !wish {
		fmt.Fprintln(w, "Add these to your wishlist with --wish.")
	}
	return nil
}
