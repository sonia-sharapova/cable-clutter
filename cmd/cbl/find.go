package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"cable-clutter/internal/catalog"
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
			printFind(os.Stdout, c, refs)
			return nil
		},
	}
}

// printFind lists matched connectors, then cables and adapters. Connectors
// that matched exactly (the first refs) also pull in every part with that end,
// so "cbl find aux" shows every cable with a 3.5 mm plug.
func printFind(w io.Writer, c *catalog.Catalog, refs []catalog.Ref) {
	var conns []*catalog.Connector
	var parts []*catalog.Part
	var signals []*catalog.Signal
	seen := map[string]bool{}
	addPart := func(p *catalog.Part) {
		if !seen[p.Key] {
			seen[p.Key] = true
			parts = append(parts, p)
		}
	}
	for i, r := range refs {
		switch r.Kind {
		case catalog.RefConnector:
			cn := c.Connectors[r.ID]
			conns = append(conns, cn)
			if i == 0 {
				for _, p := range c.ByConnector[cn.ID] {
					addPart(p)
				}
			}
		case catalog.RefCable, catalog.RefAdapter:
			p, _ := c.Part(r.ID)
			addPart(p)
		case catalog.RefSignal:
			signals = append(signals, c.Signals[r.ID])
		}
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	section := func(title string, n int) {
		if n > 0 {
			fmt.Fprintf(tw, "%s (%d)\n", title, n)
		}
	}
	section("Connectors", len(conns))
	for _, cn := range conns {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", cn.ID, cn.Name, cn.Family)
	}
	for _, kind := range []catalog.Kind{catalog.KindCable, catalog.KindAdapter} {
		var ofKind []*catalog.Part
		for _, p := range parts {
			if p.Kind == kind {
				ofKind = append(ofKind, p)
			}
		}
		title := "Cables"
		if kind == catalog.KindAdapter {
			title = "Adapters"
		}
		section(title, len(ofKind))
		for _, p := range ofKind {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", p.Name, p.Key, partFlags(p))
		}
	}
	section("Signals", len(signals))
	for _, s := range signals {
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
