// Package route finds the cheapest chain of cables and adapters joining one
// device to another. It searches a graph whose states are an open connector
// carrying a signal; each step plugs a cable or adapter into the open
// connector. A route is only valid if one signal survives the whole chain.
package route

import (
	"container/heap"
	"slices"
	"strings"

	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
	"cable-clutter/internal/plan"
)

// Step costs. Owning beats buying by a wide margin, so a route you can build
// today wins over a shorter one that needs a purchase.
const (
	CostFree   = 1
	CostInUse  = 3
	CostBuy    = 10
	DefaultMax = 3 // cables and adapters in one chain
)

// Step is one cable or adapter in a route.
type Step struct {
	Part *catalog.Part
	In   catalog.End   // the end plugged into the previous connector
	Out  catalog.End   // the end left open for the next step
	Item *library.Item // the item to use; nil when it must be bought
	Busy string        // when Item is in use: the system using it
}

// Route joins a port on one device to a port on another.
type Route struct {
	Signal string
	From   catalog.Port
	To     catalog.Port
	Steps  []Step
	Cost   int
}

// Buy lists the parts the route needs that you don't have free or in use.
func (r *Route) Buy() []*catalog.Part {
	var out []*catalog.Part
	for _, s := range r.Steps {
		if s.Item == nil {
			out = append(out, s.Part)
		}
	}
	return out
}

// SameSteps reports whether two routes use the same parts through the same ports.
func (r *Route) SameSteps(o *Route) bool {
	if r.From.Label != o.From.Label || r.To.Label != o.To.Label || len(r.Steps) != len(o.Steps) {
		return false
	}
	for i := range r.Steps {
		if r.Steps[i].Part != o.Steps[i].Part {
			return false
		}
	}
	return true
}

type Options struct {
	MaxSteps  int
	OwnedOnly bool // never suggest buying
}

// Connect finds, for each kind of signal (video, audio, data) both devices
// handle, the cheapest route from an output port of a to an input port of b.
// Power is left out: it isn't something you route between devices.
func Connect(c *catalog.Catalog, a, b *catalog.DeviceModel, inv plan.Inventories, opt Options) map[string]*Route {
	if opt.MaxSteps == 0 {
		opt.MaxSteps = DefaultMax
	}
	carries := connectorSignals(c)

	pq := &queue{}
	for _, p := range a.Ports {
		for _, s := range p.Signals {
			if p.Sends(s) && !strings.HasPrefix(s, "power.") {
				heap.Push(pq, &node{open: p.End, signal: s, from: p})
			}
		}
	}

	best := map[string]*Route{}
	settled := map[string]bool{}
	for pq.Len() > 0 {
		n := heap.Pop(pq).(*node)
		kind := c.Signals[n.signal].Kind
		if len(n.steps) > 0 {
			if _, done := best[kind]; !done {
				for _, t := range b.Ports {
					if t.Receives(n.signal) && meets(c, n.open, t.End) {
						best[kind] = &Route{Signal: n.signal, From: n.from, To: t, Steps: n.steps, Cost: n.cost}
						break
					}
				}
			}
		}
		key := n.stateKey()
		if settled[key] || len(n.steps) == opt.MaxSteps {
			continue
		}
		settled[key] = true

		for _, conn := range mates(c, n.open.Connector) {
			for _, part := range c.ByConnector[conn] {
				for i, in := range part.Ends {
					if in.Connector != conn || !meets(c, n.open, in) {
						continue
					}
					outSignals := passes(c, part, in, n.signal)
					if len(outSignals) == 0 {
						continue
					}
					item, busy, cost := pick(c, part, n.steps, inv)
					if item == nil && opt.OwnedOnly {
						continue
					}
					for j, out := range part.Ends {
						if j == i {
							continue
						}
						for _, s := range outSignals {
							if !carries[out.Connector][s] {
								continue
							}
							step := Step{Part: part, In: in, Out: out, Item: item, Busy: busy}
							heap.Push(pq, &node{
								open: out, signal: s, from: n.from,
								steps: append(slices.Clip(n.steps), step), cost: n.cost + cost,
							})
						}
					}
				}
			}
		}
	}
	return best
}

// passes returns the signals that leave part when signal enters by end in:
// the same signal through ordinary cables; the converted ones through a
// converter fed at its source; nothing when audio or video would flow
// backwards through a one-way converter.
func passes(c *catalog.Catalog, part *catalog.Part, in catalog.End, signal string) []string {
	if len(part.InSignals) > 0 && in.Connector == part.Source && slices.Contains(part.InSignals, signal) {
		return part.Signals
	}
	if !slices.Contains(part.Signals, signal) {
		return nil
	}
	if part.Source != "" && in.Connector != part.Source {
		if kind := c.Signals[signal].Kind; kind == "video" || kind == "audio" {
			return nil
		}
	}
	return []string{signal}
}

// meets reports whether two ends plug together: mating connectors, opposite
// genders, and the plug side has no more plugs than the socket side takes
// (red/white RCA into a yellow/white/red input).
func meets(c *catalog.Catalog, a, b catalog.End) bool {
	if a.Gender == b.Gender || !slices.Contains(mates(c, a.Connector), b.Connector) {
		return false
	}
	male, female := a, b
	if a.Gender == "f" {
		male, female = b, a
	}
	return male.Count <= female.Count
}

func mates(c *catalog.Catalog, conn string) []string {
	out := []string{conn}
	if cn, ok := c.Connectors[conn]; ok {
		out = append(out, cn.Mates...)
	}
	return out
}

// connectorSignals is which signals each connector can carry, learned from
// every two-ended cable and device port in the catalog. It stops a hub's
// USB-A socket being offered as an HDMI output.
func connectorSignals(c *catalog.Catalog) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	add := func(conn string, signals []string) {
		if out[conn] == nil {
			out[conn] = map[string]bool{}
		}
		for _, s := range signals {
			out[conn][s] = true
		}
	}
	for _, p := range c.Cables {
		if len(p.Ends) == 2 {
			for _, e := range p.Ends {
				add(e.Connector, p.Signals)
			}
		}
	}
	for _, d := range c.DeviceModels() {
		for _, p := range d.Ports {
			add(p.End.Connector, p.Signals)
		}
	}
	return out
}

// pick chooses which owned item a step would use, given the items earlier
// steps of the same route already use: a free one, else one in use, else
// none (buy).
func pick(c *catalog.Catalog, part *catalog.Part, earlier []Step, inv plan.Inventories) (*library.Item, string, int) {
	in := inv.Of(c, part.Key)
	used := map[string]bool{}
	for _, s := range earlier {
		if s.Item != nil {
			used[s.Item.ID] = true
		}
	}
	var busy *library.Item
	for _, it := range in.Owned[part.Key] {
		if used[it.ID] {
			continue
		}
		if in.Status[it.ID] != library.InUse {
			return it, "", CostFree
		}
		if busy == nil {
			busy = it
		}
	}
	if busy != nil {
		return busy, in.HeldBy[busy.ID], CostInUse
	}
	return nil, "", CostBuy
}

type node struct {
	open   catalog.End
	signal string
	from   catalog.Port
	steps  []Step
	cost   int
	index  int
}

// stateKey identifies a search state. Routes from different source ports
// stay distinct so each can still reach its own best target.
func (n *node) stateKey() string {
	return n.from.Label + "|" + n.open.String() + "|" + n.signal
}

type queue []*node

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return len(q[i].steps) < len(q[j].steps)
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i]; q[i].index = i; q[j].index = j }
func (q *queue) Push(x any)   { n := x.(*node); n.index = len(*q); *q = append(*q, n) }
func (q *queue) Pop() any {
	old := *q
	n := old[len(old)-1]
	*q = old[:len(old)-1]
	return n
}
