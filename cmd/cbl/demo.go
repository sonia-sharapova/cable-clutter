package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/route"
	"github.com/sonia-sharapova/cable-clutter/internal/smart"
)

// The example setups `cbl demo` fills in: a Raspberry Pi desk and a laptop
// desk. Each is safe to run again: what's already there is kept.
var demoDevices = []struct{ model, name string }{
	{"raspberry-pi-3b", "Raspberry Pi"},
	{"dell-monitor", "Dell monitor"},
	{"generic-usb-keyboard", "Keyboard"},
	{"generic-usb-mouse", "Mouse"},
	{"generic-usb-charger", "USB charger"},
	{"macbook-pro-13-m1-2020", "MacBook Pro"},
	{"te-ob-4", "TE speaker"},
}

type demoSystem struct {
	name string
	hops [][2]string // device → device, planned like `cbl plan`
	// Power isn't routed between devices, so a charging cable is a link
	// named outright: part, from device · port, to device · port.
	power []linkRequest
}

var demoSystems = []demoSystem{
	{
		name: "Pi desk",
		hops: [][2]string{{"Raspberry Pi", "Dell monitor"}, {"Keyboard", "Raspberry Pi"}, {"Mouse", "Raspberry Pi"}},
		power: []linkRequest{{Part: "micro-usb:m|usb-a:m", FromDevice: "USB charger", FromPort: "USB-A",
			ToDevice: "Raspberry Pi", ToPort: "Power"}},
	},
	{
		name: "Laptop desk",
		hops: [][2]string{{"MacBook Pro", "Dell monitor"}, {"MacBook Pro", "TE speaker"}},
	},
}

func demoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Fill in two example setups: a Raspberry Pi desk and a laptop desk",
		Long: `Adds the devices, cables and systems for two example setups, so there is
something to look at straight away:

  Pi desk      Raspberry Pi → monitor (HDMI), a keyboard and a mouse plugged
               into the Pi, and a USB charger powering it
  Laptop desk  MacBook Pro → monitor, and → a speaker over aux

Devices and systems you already have are kept and added to; cables are
added only when a system would otherwise be short of one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := loadState()
			if err != nil {
				return err
			}
			defer st.db.Close()
			done, err := st.seedDemo()
			if err != nil {
				return err
			}
			if jsonOut {
				return emit(cmd, map[string]any{"added": done})
			}
			out := cmd.OutOrStdout()
			if len(done) == 0 {
				fmt.Fprintln(out, "The example setups are already there.")
			}
			for _, line := range done {
				fmt.Fprintln(out, line)
			}
			fmt.Fprintln(out, "\nSee them with `cbl system show \"Pi desk\"`, or in `cbl serve` under Your network → Systems.")
			return nil
		},
	}
}

// seedDemo adds what the example setups lack and says what it added.
func (st *state) seedDemo() ([]string, error) {
	var done []string
	for _, d := range demoDevices {
		if len(library.FindDevices(st.devices, d.name)) > 0 {
			continue
		}
		if _, ok := st.c.Devices[d.model]; !ok {
			return nil, fmt.Errorf("the catalog has no %s; update cbl", d.model)
		}
		if _, _, err := st.db.AddDevice(d.model, d.name, ""); err != nil {
			return nil, err
		}
		done = append(done, "Added device "+d.name)
	}
	if err := st.reload(); err != nil {
		return nil, err
	}

	for _, ds := range demoSystems {
		s, err := st.system(ds.name)
		if err != nil {
			if s, err = st.db.NewSystem(ds.name, ""); err != nil {
				return nil, err
			}
			done = append(done, "Created system "+ds.name)
		}
		for _, h := range ds.hops {
			if st.joined(s, h[0], h[1]) {
				continue
			}
			a, err := st.ownedDevice(h[0])
			if err != nil {
				return nil, err
			}
			b, err := st.ownedDevice(h[1])
			if err != nil {
				return nil, err
			}
			hops := smart.Chain(st.c, []smart.Device{a, b}, st.inv, route.Options{})
			if len(hops) == 0 || len(hops[0].Routes) == 0 {
				return nil, fmt.Errorf("no way to connect %s to %s", h[0], h[1])
			}
			if _, err := st.whileOn(s, func() error { return st.addHops(s, hops, false) }); err != nil {
				return nil, err
			}
			done = append(done, fmt.Sprintf("  %s: %s → %s", ds.name, h[0], h[1]))
			if err := st.reload(); err != nil {
				return nil, err
			}
		}
		for _, p := range ds.power {
			if st.joinedAt(s, p.ToDevice, p.ToPort) {
				continue
			}
			l, err := st.link(s.ID, p)
			if err != nil {
				return nil, err
			}
			if _, err := st.whileOn(s, func() error { _, err := st.db.AddLink(l); return err }); err != nil {
				return nil, err
			}
			done = append(done, fmt.Sprintf("  %s: %s charges %s", ds.name, p.FromDevice, p.ToDevice))
			if err := st.reload(); err != nil {
				return nil, err
			}
		}
		// Own enough of every cable it uses, so it can be switched on.
		for _, m := range plan.Short(st.c, st.links[s.ID], st.inv) {
			p, ok := st.c.Part(m.PartKey)
			if !ok {
				continue
			}
			if _, err := st.db.AddItems(library.Item{Kind: p.Kind, PartKey: p.Key}, m.Need-m.Owned); err != nil {
				return nil, err
			}
			done = append(done, fmt.Sprintf("Added %d × %s to your inventory (for %s)", m.Need-m.Owned, p.Name, ds.name))
		}
		if err := st.reload(); err != nil {
			return nil, err
		}
	}
	return done, nil
}

// joined reports whether system s already has a link between devices a
// and b, in either direction, possibly through other cables.
func (st *state) joined(s *library.System, a, b string) bool {
	for _, chain := range linkChains(st.links[s.ID]) {
		first, last := chain[0], chain[len(chain)-1]
		ends := []string{strings.ToLower(first.FromDevice), strings.ToLower(last.ToDevice)}
		if (ends[0] == strings.ToLower(a) && ends[1] == strings.ToLower(b)) ||
			(ends[0] == strings.ToLower(b) && ends[1] == strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// joinedAt reports whether something in s already plugs into that port.
func (st *state) joinedAt(s *library.System, device, port string) bool {
	for _, l := range st.links[s.ID] {
		if (strings.EqualFold(l.ToDevice, device) && strings.EqualFold(l.ToPort, port)) ||
			(strings.EqualFold(l.FromDevice, device) && strings.EqualFold(l.FromPort, port)) {
			return true
		}
	}
	return false
}
