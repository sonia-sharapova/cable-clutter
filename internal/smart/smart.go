// Package smart answers questions across all your gear at once: what each
// device can reach, how to wire a chain of devices, which single purchase
// helps most, and which cables are dead weight or worth a spare.
package smart

import (
	"maps"
	"slices"
	"sort"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/route"
)

// Device is one of the user's devices with a known model.
type Device struct {
	Nickname string
	Model    *catalog.DeviceModel
}

// Devices pairs the user's devices with their catalog models, skipping
// placeholders that have none yet.
func Devices(c *catalog.Catalog, ds []*library.Device) []Device {
	var out []Device
	for _, d := range ds {
		if m, ok := c.Devices[d.Model]; ok {
			out = append(out, Device{d.Nickname, m})
		}
	}
	return out
}

// Kinds lists signal kinds in display order.
var Kinds = []string{"video", "audio", "data"}

// Reach is what one device can do with another using only owned items.
type Reach struct {
	Other    Device
	Outgoing map[string]*route.Route // from the device to Other, by kind
	Incoming map[string]*route.Route // from Other to the device, by kind
}

// ReachFrom lists, for every other device, the routes you can build today
// in each direction. Devices with no route either way are left out.
func ReachFrom(c *catalog.Catalog, d Device, all []Device, inv plan.Inventories) []Reach {
	opt := route.Options{OwnedOnly: true}
	var out []Reach
	for _, o := range all {
		if o.Nickname == d.Nickname {
			continue
		}
		r := Reach{Other: o, Outgoing: route.Connect(c, d.Model, o.Model, inv, opt), Incoming: route.Connect(c, o.Model, d.Model, inv, opt)}
		if len(r.Outgoing)+len(r.Incoming) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Hop is one leg of a chain: the routes from one device to the next.
type Hop struct {
	From, To Device
	Routes   map[string]*route.Route
}

// Chain wires devices in order (A → B → C …) so that no physical item is
// used for two jobs. One cable carrying both video and audio on a hop (the
// PS2 AV cable) is one job. Hops are planned in order, each taking the best
// route from what earlier hops left.
func Chain(c *catalog.Catalog, devices []Device, inv plan.Inventories, opt route.Options) []Hop {
	taken := map[string]bool{}
	var hops []Hop
	for i := 0; i+1 < len(devices); i++ {
		a, b := devices[i], devices[i+1]
		hop := Hop{From: a, To: b, Routes: map[string]*route.Route{}}
		used := maps.Clone(taken)
		for _, kind := range Kinds {
			// First see whether a route already chosen on this hop carries it.
			o := opt
			o.Exclude = taken
			r := route.Connect(c, a.Model, b.Model, inv, o)[kind]
			if r == nil {
				continue
			}
			shared := false
			for _, prev := range hop.Routes {
				shared = shared || prev.SameSteps(r)
			}
			if !shared {
				o.Exclude = used
				if r = route.Connect(c, a.Model, b.Model, inv, o)[kind]; r == nil {
					continue
				}
			}
			hop.Routes[kind] = r
			for _, s := range r.Steps {
				if s.Item != nil {
					used[s.Item.ID] = true
				}
			}
		}
		taken = used
		hops = append(hops, hop)
	}
	return hops
}

// Unlock is a part you don't own and the connections it would make possible.
type Unlock struct {
	Part  *catalog.Part
	Pairs []Pair
}

// Pair is a device-to-device connection of one signal kind.
type Pair struct {
	From, To string
	Kind     string
}

// BestBuy ranks parts by how many new connections between your devices
// owning one would allow. A route needing no purchase is always cheaper than
// one needing any (see route costs), so a best route with exactly one
// purchase means that part unlocks the connection.
func BestBuy(c *catalog.Catalog, devices []Device, inv plan.Inventories) []Unlock {
	byPart := map[string]*Unlock{}
	for _, a := range devices {
		for _, b := range devices {
			if a.Nickname == b.Nickname {
				continue
			}
			for kind, r := range route.Connect(c, a.Model, b.Model, inv, route.Options{}) {
				buy := r.Buy()
				if len(buy) != 1 {
					continue
				}
				u := byPart[buy[0].Key]
				if u == nil {
					u = &Unlock{Part: buy[0]}
					byPart[buy[0].Key] = u
				}
				u.Pairs = append(u.Pairs, Pair{a.Nickname, b.Nickname, kind})
			}
		}
	}
	out := make([]Unlock, 0, len(byPart))
	for _, u := range byPart {
		sort.Slice(u.Pairs, func(i, j int) bool {
			if u.Pairs[i].From != u.Pairs[j].From {
				return u.Pairs[i].From < u.Pairs[j].From
			}
			if u.Pairs[i].To != u.Pairs[j].To {
				return u.Pairs[i].To < u.Pairs[j].To
			}
			return u.Pairs[i].Kind < u.Pairs[j].Kind
		})
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Pairs) != len(out[j].Pairs) {
			return len(out[i].Pairs) > len(out[j].Pairs)
		}
		return out[i].Part.Name < out[j].Part.Name
	})
	return out
}

// Unused lists the part types you own that can't reach any of your devices,
// with the items of each: candidates to give away, or a sign a device is
// missing from your library. A part is useful if it plugs into a device port
// (at the right voltage), or into the free end of another useful part: a
// wall charger is useful because a USB cable whose other end is in your PSP
// plugs into it. The end a part uses to attach never counts as free, so
// chaining plug adapters at the wall doesn't make an unused adaptor useful.
func Unused(c *catalog.Catalog, devices []Device, inv plan.Inventories) map[string][]*library.Item {
	owned := map[string]*catalog.Part{}
	for _, in := range []*library.Inventory{inv.Cables, inv.Adapters} {
		for key := range in.Owned {
			if p, ok := c.Part(key); ok {
				owned[key] = p
			}
		}
	}
	useful := map[string]bool{}
	free := map[string]map[int]bool{} // part key -> indexes of ends left open
	attach := func(p *catalog.Part, at int) bool {
		grew := !useful[p.Key]
		useful[p.Key] = true
		if free[p.Key] == nil {
			free[p.Key] = map[int]bool{}
		}
		for i := range p.Ends {
			if i != at && !free[p.Key][i] {
				free[p.Key][i], grew = true, true
			}
		}
		return grew
	}
	for _, d := range devices {
		for _, port := range d.Model.Ports {
			for _, f := range c.Fits(port) {
				if p, ok := owned[f.Part.Key]; ok && !f.WrongVoltage {
					attach(p, slices.Index(p.Ends, f.End))
				}
			}
		}
	}
	for grew := true; grew; {
		grew = false
		for _, p := range owned {
			for i, e := range p.Ends {
				for qKey, ends := range free {
					q := owned[qKey]
					for j := range ends {
						if q != p && c.Meets(e, q.Ends[j]) && attach(p, i) {
							grew = true
						}
					}
				}
			}
		}
	}
	out := map[string][]*library.Item{}
	for _, in := range []*library.Inventory{inv.Cables, inv.Adapters} {
		for key, items := range in.Owned {
			if !useful[key] {
				out[key] = items
			}
		}
	}
	return out
}

// WorthASpare lists part types whose shortage stops systems being up
// together, with the pairs of systems a second one would free.
func WorthASpare(c *catalog.Catalog, ports plan.Ports, systems []*library.System, links map[string][]*library.Link, inv plan.Inventories) map[string][][2]string {
	out := map[string][][2]string{}
	conflicts := plan.Conflicts(c, ports, systems, links, inv)
	for _, s := range systems {
		for _, cf := range conflicts[s.ID] {
			if cf.PartKey != "" && s.Name < cf.Other.Name { // each pair once
				out[cf.PartKey] = append(out[cf.PartKey], [2]string{s.Name, cf.Other.Name})
			}
		}
	}
	return out
}
