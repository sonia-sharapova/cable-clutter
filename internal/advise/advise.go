// Package advise answers "what can I do with this device, using the cables
// and adapters I own, and what am I missing?" one port at a time.
package advise

import (
	"slices"
	"sort"
	"strings"

	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
)

// Owned is a part type that fits a port, with the items of it you own.
type Owned struct {
	Fit       catalog.Fit
	Items     []*library.Item
	Free      int
	Dedicated bool // at least one item belongs to this device
}

// Suggestion is a part you don't own that would add signals to a port.
type Suggestion struct {
	Part    *catalog.Part
	Signals []string // the missing signals it adds
}

type PortReport struct {
	Port    catalog.Port
	Have    []Owned      // owned and safe to use
	Can     []string     // port signals covered by what you own
	CantYet []Suggestion // signals you can't use yet, and what would add them
	Wrong   []Owned      // owned power supplies of the wrong voltage
	Unsure  []Owned      // owned power supplies of unknown voltage
}

// Device reports on every port of model. nickname is the user's name for the
// device (to put its dedicated cables first), or "".
func Device(c *catalog.Catalog, model *catalog.DeviceModel, cables, adapters *library.Inventory, nickname string) []PortReport {
	var out []PortReport
	for _, port := range model.Ports {
		out = append(out, portReport(c, port, cables, adapters, nickname))
	}
	return out
}

func portReport(c *catalog.Catalog, port catalog.Port, cables, adapters *library.Inventory, nickname string) PortReport {
	r := PortReport{Port: port}
	covered := map[string]bool{}
	var notOwned []catalog.Fit
	for _, f := range c.Fits(port) {
		inv := cables
		if f.Part.Kind == catalog.KindAdapter {
			inv = adapters
		}
		items := inv.Owned[f.Part.Key]
		if len(items) == 0 {
			if !f.WrongVoltage && !unknownVoltage(port, f.Part) {
				notOwned = append(notOwned, f)
			}
			continue
		}
		o := Owned{Fit: f, Items: items}
		for _, it := range items {
			if inv.Status[it.ID] == library.Free {
				o.Free++
			}
			if nickname != "" && strings.EqualFold(it.Dedicated, nickname) {
				o.Dedicated = true
			}
		}
		switch {
		case f.WrongVoltage:
			r.Wrong = append(r.Wrong, o)
		case unknownVoltage(port, f.Part):
			r.Unsure = append(r.Unsure, o)
		default:
			r.Have = append(r.Have, o)
			for _, s := range f.Signals {
				covered[s] = true
			}
		}
	}
	// Dedicated first, then the most capable, then by name.
	sort.SliceStable(r.Have, func(i, j int) bool {
		a, b := r.Have[i], r.Have[j]
		if a.Dedicated != b.Dedicated {
			return a.Dedicated
		}
		if len(a.Fit.Signals) != len(b.Fit.Signals) {
			return len(a.Fit.Signals) > len(b.Fit.Signals)
		}
		return a.Fit.Part.Name < b.Fit.Part.Name
	})
	for _, s := range port.Signals {
		if covered[s] {
			r.Can = append(r.Can, s)
		}
	}
	r.CantYet = suggest(port, covered, notOwned)
	return r
}

// unknownVoltage: a power port with a rated voltage, and a power supply
// whose voltage isn't known. It may fit; it may also damage the device.
func unknownVoltage(port catalog.Port, p *catalog.Part) bool {
	return port.Volts > 0 && p.Volts == 0 && slices.Contains(p.Signals, "power.dc")
}

// suggest picks, for the signals nobody owns a part for, the fewest
// purchases that add them: greedily the part adding the most missing
// signals, preferring passive parts with fewer ends.
func suggest(port catalog.Port, covered map[string]bool, fits []catalog.Fit) []Suggestion {
	// Power is power: once any source powers the port, other kinds of power
	// (USB vs a DC adaptor) are alternatives, not something missing.
	powered := false
	for s := range covered {
		powered = powered || strings.HasPrefix(s, "power.")
	}
	missing := map[string]bool{}
	for _, s := range port.Signals {
		if !covered[s] && !(powered && strings.HasPrefix(s, "power.")) {
			missing[s] = true
		}
	}
	var out []Suggestion
	for len(missing) > 0 {
		var best *catalog.Fit
		var bestAdds []string
		for i := range fits {
			f := &fits[i]
			var adds []string
			for _, s := range f.Signals {
				if missing[s] {
					adds = append(adds, s)
				}
			}
			if len(adds) == 0 {
				continue
			}
			if best == nil || better(f, adds, best, bestAdds) {
				best, bestAdds = f, adds
			}
		}
		if best == nil {
			break // nothing in the catalog carries the rest
		}
		out = append(out, Suggestion{Part: best.Part, Signals: bestAdds})
		for _, s := range bestAdds {
			delete(missing, s)
		}
	}
	return out
}

func better(f *catalog.Fit, adds []string, best *catalog.Fit, bestAdds []string) bool {
	if len(adds) != len(bestAdds) {
		return len(adds) > len(bestAdds)
	}
	if f.Part.Active != best.Part.Active {
		return !f.Part.Active
	}
	if len(f.Part.Ends) != len(best.Part.Ends) {
		return len(f.Part.Ends) < len(best.Part.Ends)
	}
	return f.Part.Name < best.Part.Name
}
