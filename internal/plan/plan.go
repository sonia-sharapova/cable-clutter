// Package plan decides which physical cables a system uses when it comes up,
// and which systems can't be up at the same time.
package plan

import (
	"sort"
	"strings"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

// Pick is the item chosen for one link.
type Pick struct {
	Link    *library.Link
	Item    *library.Item
	Swapped bool // the link pins another item, which is busy
}

// Shortage is a link with no free item of its type.
type Shortage struct {
	Link   *library.Link
	Owned  int      // items of this type you own
	HeldBy []string // active systems using them, sorted
}

// Inventories holds both item kinds; Of picks the one a part key belongs to.
type Inventories struct {
	Cables, Adapters *library.Inventory
}

func (inv Inventories) Of(c *catalog.Catalog, key string) *library.Inventory {
	if p, ok := c.Part(key); ok && p.Kind == catalog.KindAdapter {
		return inv.Adapters
	}
	return inv.Cables
}

// Allocate chooses an item for every link from items not in use elsewhere.
// For each link it takes, in order: the pinned item; an item dedicated to one
// of the link's devices; a spare nobody pins; one another system pins; and
// last, one dedicated to some other device.
func Allocate(c *catalog.Catalog, links []*library.Link, inv Inventories) ([]Pick, []Shortage) {
	taken := map[string]bool{}
	var picks []Pick
	var short []Shortage
	for _, l := range links {
		in := inv.Of(c, l.PartKey)
		var best *library.Item
		bestRank := 99
		for _, it := range in.Owned[l.PartKey] {
			if taken[it.ID] || in.Status[it.ID] == library.InUse {
				continue
			}
			if r := rank(l, it, in); r < bestRank {
				best, bestRank = it, r
			}
		}
		if best == nil {
			s := Shortage{Link: l, Owned: len(in.Owned[l.PartKey])}
			held := map[string]bool{}
			for _, it := range in.Owned[l.PartKey] {
				if h := in.HeldBy[it.ID]; h != "" && !held[h] {
					held[h] = true
					s.HeldBy = append(s.HeldBy, h)
				}
			}
			sort.Strings(s.HeldBy)
			short = append(short, s)
			continue
		}
		taken[best.ID] = true
		picks = append(picks, Pick{Link: l, Item: best, Swapped: l.Preferred != "" && best.ID != l.Preferred})
	}
	return picks, short
}

func rank(l *library.Link, it *library.Item, in *library.Inventory) int {
	switch {
	case it.ID == l.Preferred:
		return 0
	case it.Dedicated != "" && (strings.EqualFold(it.Dedicated, l.FromDevice) || strings.EqualFold(it.Dedicated, l.ToDevice)):
		return 1
	case it.Dedicated == "" && in.Status[it.ID] == library.Free:
		return 2
	case it.Dedicated == "":
		return 3
	}
	return 4
}

// Conflict says two systems can't both be up: together they need more of a
// part type than you own, or more of a device's port than it has.
type Conflict struct {
	Other   *library.System
	PartKey string // set for a cable or adapter shortfall
	Port    string // "device · port", set for a port clash
	Need    int    // both systems together
	Have    int    // owned items, or ports of that kind on the device
}

// Ports knows how many of each device port exist: models maps a device
// nickname (lower case) to its catalog model id.
type Ports struct {
	C      *catalog.Catalog
	Models map[string]string
}

// NewPorts indexes the user's devices by nickname.
func NewPorts(c *catalog.Catalog, devices []*library.Device) Ports {
	m := map[string]string{}
	for _, d := range devices {
		m[strings.ToLower(d.Nickname)] = d.Model
	}
	return Ports{C: c, Models: m}
}

// capacity is how many identical ports the device has (0 if unknown).
func (p Ports) capacity(device, port string) int {
	m, ok := p.C.Devices[p.Models[strings.ToLower(device)]]
	if !ok {
		return 0
	}
	for _, pt := range m.Ports {
		if strings.EqualFold(pt.Label, port) {
			return pt.Qty
		}
	}
	return 0
}

// portUse counts how many links of a system plug into each known port.
func portUse(links []*library.Link) map[string]int {
	use := map[string]int{}
	for _, l := range links {
		if l.FromDevice != "" && l.FromPort != "" {
			use[l.FromDevice+" · "+l.FromPort]++
		}
		if l.ToDevice != "" && l.ToPort != "" {
			use[l.ToDevice+" · "+l.ToPort]++
		}
	}
	return use
}

func (p Ports) capacityOf(key string) int {
	device, port, _ := strings.Cut(key, " · ")
	return p.capacity(device, port)
}

// Conflicts returns, for each system id, the systems it can't be up with.
// A system that alone needs more than you own is reported by Short instead.
func Conflicts(c *catalog.Catalog, ports Ports, systems []*library.System, links map[string][]*library.Link, inv Inventories) map[string][]Conflict {
	demand := map[string]map[string]int{} // system -> part key -> links
	use := map[string]map[string]int{}    // system -> port -> links
	for _, s := range systems {
		demand[s.ID] = map[string]int{}
		for _, l := range links[s.ID] {
			demand[s.ID][l.PartKey]++
		}
		use[s.ID] = portUse(links[s.ID])
	}
	out := map[string][]Conflict{}
	add := func(a, b *library.System, cf Conflict) {
		cf.Other = b
		out[a.ID] = append(out[a.ID], cf)
		cf.Other = a
		out[b.ID] = append(out[b.ID], cf)
	}
	for i, a := range systems {
		for _, b := range systems[i+1:] {
			if cf, ok := clash(demand[a.ID], demand[b.ID], func(key string) int { return len(inv.Of(c, key).Owned[key]) }); ok {
				add(a, b, Conflict{PartKey: cf.key, Need: cf.need, Have: cf.have})
			} else if cf, ok := clash(use[a.ID], use[b.ID], ports.capacityOf); ok {
				add(a, b, Conflict{Port: cf.key, Need: cf.need, Have: cf.have})
			}
		}
	}
	return out
}

type shortfall struct {
	key        string
	need, have int
}

// clash finds a key both systems use whose combined need exceeds what
// exists, when each alone fits. Keys with unknown capacity (0) never clash.
func clash(a, b map[string]int, have func(string) int) (shortfall, bool) {
	for _, key := range sortedKeys(a) {
		da, db := a[key], b[key]
		h := have(key)
		if db == 0 || h == 0 || da > h || db > h || da+db <= h {
			continue
		}
		return shortfall{key, da + db, h}, true
	}
	return shortfall{}, false
}

// PortClash is a device port this system needs that active systems fill.
type PortClash struct {
	Port   string   `json:"port"` // "device · port"
	HeldBy []string `json:"held_by"`
}

// BusyPorts lists the ports system s would need that active systems already
// fill to capacity.
func BusyPorts(ports Ports, s *library.System, systems []*library.System, links map[string][]*library.Link) []PortClash {
	need := portUse(links[s.ID])
	var out []PortClash
	for _, port := range sortedKeys(need) {
		capacity := ports.capacityOf(port)
		if capacity == 0 {
			continue
		}
		used, held := 0, []string{}
		for _, o := range systems {
			if o.ID == s.ID || !o.Active {
				continue
			}
			if n := portUse(links[o.ID])[port]; n > 0 {
				used += n
				held = append(held, o.Name)
			}
		}
		if used+need[port] > capacity {
			out = append(out, PortClash{Port: port, HeldBy: held})
		}
	}
	return out
}

// Missing is a part type a system needs more of than you own at all.
type Missing struct {
	PartKey string `json:"part"`
	Need    int    `json:"need"`
	Owned   int    `json:"owned"`
}

// Short lists what a system can never come up without buying.
func Short(c *catalog.Catalog, links []*library.Link, inv Inventories) []Missing {
	need := map[string]int{}
	for _, l := range links {
		need[l.PartKey]++
	}
	var out []Missing
	for _, key := range sortedKeys(need) {
		if owned := len(inv.Of(c, key).Owned[key]); need[key] > owned {
			out = append(out, Missing{PartKey: key, Need: need[key], Owned: owned})
		}
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
