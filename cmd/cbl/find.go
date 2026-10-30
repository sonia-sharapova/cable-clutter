package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
)

func findCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "find <text>",
		Short: "Search connectors, cables, adapters and signals by name or alias",
		Example: `  cbl find aux
  cbl find "usb c"
  cbl find lightning`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := loadCatalog()
			if err != nil {
				return err
			}
			q := strings.Join(args, " ")
			start := time.Now()
			refs := c.Find(q)
			report("find", start)
			if len(refs) == 0 {
				return fmt.Errorf("nothing matches %q", q)
			}
			g := groupFind(c, refs)
			if jsonOut {
				return emit(cmd, map[string]any{
					"connectors": g.conns, "cables": viewParts(g.ofKind(catalog.KindCable)),
					"adapters": viewParts(g.ofKind(catalog.KindAdapter)), "devices": g.devices, "signals": g.signals,
				})
			}
			printFind(cmd.OutOrStdout(), g)
			return nil
		},
	}
}

type findGroups struct {
	conns   []*catalog.Connector
	parts   []*catalog.Part
	signals []*catalog.Signal
	devices []*catalog.DeviceModel
}

func (g findGroups) ofKind(k catalog.Kind) []*catalog.Part {
	out := []*catalog.Part{}
	for _, p := range g.parts {
		if p.Kind == k {
			out = append(out, p)
		}
	}
	return out
}

// groupFind sorts matches by type. Connectors that matched exactly (the first
// refs) also pull in every part with that end, so "cbl find aux" shows every
// cable with a 3.5 mm plug.
func groupFind(c *catalog.Catalog, refs []catalog.Ref) findGroups {
	var g findGroups
	seen := map[string]bool{}
	addPart := func(p *catalog.Part) {
		if !seen[p.Key] {
			seen[p.Key] = true
			g.parts = append(g.parts, p)
		}
	}
	for i, r := range refs {
		switch r.Kind {
		case catalog.RefConnector:
			cn := c.Connectors[r.ID]
			g.conns = append(g.conns, cn)
			if i == 0 {
				for _, p := range c.ByConnector[cn.ID] {
					addPart(p)
				}
			}
		case catalog.RefCable, catalog.RefAdapter:
			p, _ := c.Part(r.ID)
			addPart(p)
		case catalog.RefSignal:
			g.signals = append(g.signals, c.Signals[r.ID])
		case catalog.RefDevice:
			g.devices = append(g.devices, c.Devices[r.ID])
		}
	}
	return g
}

// printFind lists matched connectors, then cables, adapters, devices and signals.
func printFind(w io.Writer, g findGroups) {

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	section := func(title string, n int) {
		if n > 0 {
			fmt.Fprintf(tw, "%s (%d)\n", title, n)
		}
	}
	section("Connectors", len(g.conns))
	for _, cn := range g.conns {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", cn.ID, cn.Name, cn.Family)
	}
	for _, kind := range []catalog.Kind{catalog.KindCable, catalog.KindAdapter} {
		ofKind := g.ofKind(kind)
		title := "Cables"
		if kind == catalog.KindAdapter {
			title = "Adapters"
		}
		section(title, len(ofKind))
		for _, p := range ofKind {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", p.Name, p.Key, partFlags(p))
		}
	}
	section("Devices", len(g.devices))
	for _, d := range g.devices {
		fmt.Fprintf(tw, "  %s\t%s\t\n", d.Name, d.ID)
	}
	section("Signals", len(g.signals))
	for _, s := range g.signals {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", s.ID, s.Name, s.Kind)
	}
	tw.Flush()
}

func partFlags(p *catalog.Part) string {
	var f []string
	if p.Active {
		f = append(f, "active")
	}
	if p.Volts > 0 {
		f = append(f, fmt.Sprintf("%gV", p.Volts))
	}
	if p.Amps > 0 {
		f = append(f, fmt.Sprintf("%gA", p.Amps))
	}
	return strings.Join(f, " ")
}
