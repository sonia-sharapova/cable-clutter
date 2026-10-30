// Package graph builds the graph of your gear: devices and connector types
// as nodes; device ports, owned cables and adapters, and connectors that
// physically mate as edges. It renders as Mermaid or Graphviz DOT.
package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/smart"
)

type EdgeKind int

const (
	Port    EdgeKind = iota // device — connector it has
	Cable                   // an owned cable between two connectors
	Adapter                 // an owned adapter between two connectors
	Fits                    // two connector types that plug together
)

type Node struct {
	ID     string // "dev:<nickname>" or "conn:<connector id>"
	Label  string
	Device bool
}

type Edge struct {
	From, To string // node ids
	Kind     EdgeKind
	Label    string // port label, item count, or "fits"
	Part     string // the cable or adapter's name
}

type Graph struct {
	Nodes []Node
	Edges []Edge
}

// Build makes the graph of your devices and owned parts. Multi-ended parts
// (hubs) join their first end to each other end.
func Build(c *catalog.Catalog, devices []smart.Device, inv plan.Inventories) *Graph {
	g := &Graph{}
	nodes := map[string]bool{}
	conn := func(id string) string {
		nid := "conn:" + id
		if !nodes[nid] {
			nodes[nid] = true
			label := id
			if cn, ok := c.Connectors[id]; ok {
				label, _, _ = strings.Cut(cn.Name, " (")
			}
			g.Nodes = append(g.Nodes, Node{ID: nid, Label: label})
		}
		return nid
	}

	for _, d := range devices {
		id := "dev:" + d.Nickname
		g.Nodes = append(g.Nodes, Node{ID: id, Label: d.Nickname, Device: true})
		nodes[id] = true
		seen := map[string]bool{}
		for _, p := range d.Model.Ports {
			if !seen[p.End.Connector] {
				seen[p.End.Connector] = true
				g.Edges = append(g.Edges, Edge{From: id, To: conn(p.End.Connector), Kind: Port, Label: p.Label})
			}
		}
	}
	for _, src := range []struct {
		owned map[string][]*library.Item
		kind  EdgeKind
	}{{inv.Cables.Owned, Cable}, {inv.Adapters.Owned, Adapter}} {
		for _, key := range sortedKeys(src.owned) {
			p, ok := c.Part(key)
			if !ok {
				continue
			}
			label := ""
			if n := len(src.owned[key]); n > 1 {
				label = fmt.Sprintf("x%d", n)
			}
			first := conn(p.Ends[0].Connector)
			for _, e := range p.Ends[1:] {
				g.Edges = append(g.Edges, Edge{From: first, To: conn(e.Connector), Kind: src.kind, Label: label, Part: p.Name})
			}
		}
	}
	// Mating connectors, between nodes already in the graph.
	for _, cn := range c.Connectors {
		for _, m := range cn.Mates {
			if cn.ID < m && nodes["conn:"+cn.ID] && nodes["conn:"+m] {
				g.Edges = append(g.Edges, Edge{From: "conn:" + cn.ID, To: "conn:" + m, Kind: Fits, Label: "fits"})
			}
		}
	}
	sort.SliceStable(g.Edges, func(i, j int) bool { return g.Edges[i].Kind < g.Edges[j].Kind })
	return g
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Around keeps only the part of the graph connected to node id.
func (g *Graph) Around(id string) *Graph {
	adj := map[string][]string{}
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	keep := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, m := range adj[n] {
			if !keep[m] {
				keep[m] = true
				queue = append(queue, m)
			}
		}
	}
	out := &Graph{}
	for _, n := range g.Nodes {
		if keep[n.ID] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range g.Edges {
		if keep[e.From] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

// Islands lists devices with no owned cable or adapter on any of their
// connectors: nothing you own plugs into them.
func (g *Graph) Islands() []string {
	touched := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind == Cable || e.Kind == Adapter || e.Kind == Fits {
			touched[e.From], touched[e.To] = true, true
		}
	}
	var out []string
	for _, n := range g.Nodes {
		if !n.Device {
			continue
		}
		alone := true
		for _, e := range g.Edges {
			if e.Kind == Port && e.From == n.ID && touched[e.To] {
				alone = false
			}
		}
		if alone {
			out = append(out, n.Label)
		}
	}
	return out
}

// Mermaid renders a flowchart: devices as boxes, connectors as circles,
// ports thin, owned cables thick, adapters and mating plugs dotted.
func (g *Graph) Mermaid() string {
	ids := g.shortIDs()
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range g.Nodes {
		if n.Device {
			fmt.Fprintf(&b, "  %s[%q]\n", ids[n.ID], n.Label)
		} else {
			fmt.Fprintf(&b, "  %s((%q))\n", ids[n.ID], n.Label)
		}
	}
	for _, e := range g.Edges {
		from, to := ids[e.From], ids[e.To]
		switch {
		case e.Kind == Port:
			fmt.Fprintf(&b, "  %s --- %s\n", from, to)
		case e.Kind == Cable && e.Label != "":
			fmt.Fprintf(&b, "  %s == %s === %s\n", from, e.Label, to)
		case e.Kind == Cable:
			fmt.Fprintf(&b, "  %s === %s\n", from, to)
		case e.Label != "":
			fmt.Fprintf(&b, "  %s -. %s .- %s\n", from, e.Label, to)
		default:
			fmt.Fprintf(&b, "  %s -.- %s\n", from, to)
		}
	}
	return b.String()
}

// DOT renders Graphviz: `cbl graph --format dot | dot -Tsvg > gear.svg`.
func (g *Graph) DOT() string {
	var b strings.Builder
	b.WriteString("graph gear {\n  rankdir=LR;\n  node [fontname=Helvetica];\n")
	for _, n := range g.Nodes {
		shape := "ellipse"
		if n.Device {
			shape = "box"
		}
		fmt.Fprintf(&b, "  %q [label=%q, shape=%s];\n", n.ID, n.Label, shape)
	}
	for _, e := range g.Edges {
		style := map[EdgeKind]string{Port: "solid", Cable: "bold", Adapter: "dashed", Fits: "dotted"}[e.Kind]
		fmt.Fprintf(&b, "  %q -- %q [style=%s, label=%q];\n", e.From, e.To, style, e.Label)
	}
	b.WriteString("}\n")
	return b.String()
}

// shortIDs gives Mermaid-safe node ids: n0, n1, …
func (g *Graph) shortIDs() map[string]string {
	ids := map[string]string{}
	for i, n := range g.Nodes {
		ids[n.ID] = fmt.Sprintf("n%d", i)
	}
	return ids
}
