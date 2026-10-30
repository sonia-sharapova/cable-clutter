package catalog

import (
	"fmt"
	"slices"
)

// DeviceModel is a kind of device and the ports it has.
type DeviceModel struct {
	ID         string
	Name       string
	Aliases    []string
	Confirmed  bool   // false: ports rest on an assumption about the model
	Assumption string // which assumption, when not confirmed
	Ports      []Port
}

// Port is one connector on a device. End is the device's own connector, so a
// cable fits when its end has the same connector and the opposite gender.
type Port struct {
	Label   string
	End     End
	Qty     int    // identical ports of this kind
	Dir     string // in, out or both
	Signals []string
	OutOnly []string // signals that only leave a two-way port
	Volts   float64  // power inputs only
	Note    string
}

// Sends reports whether signal s can leave the port.
func (p Port) Sends(s string) bool { return p.Dir != "in" && slices.Contains(p.Signals, s) }

// Receives reports whether signal s can come into the port.
func (p Port) Receives(s string) bool {
	return p.Dir != "out" && slices.Contains(p.Signals, s) && !slices.Contains(p.OutOnly, s)
}

type rawPort struct {
	Label   string
	End     string
	Qty     int
	Dir     string
	Signals []string
	OutOnly []string `yaml:"out_only"`
	Volts   float64
	Note    string
}

type rawDevice struct {
	ID         string
	Name       string
	Aliases    []string
	Confirmed  bool
	Assumption string
	Ports      []rawPort
}

func (c *Catalog) addDevices(raws []rawDevice) []error {
	var errs []error
	for _, r := range raws {
		if _, dup := c.Devices[r.ID]; dup {
			errs = append(errs, fmt.Errorf("device %q defined twice", r.ID))
			continue
		}
		if r.ID == "" || r.Name == "" {
			errs = append(errs, fmt.Errorf("device %q: needs an id and a name", r.ID))
			continue
		}
		if !r.Confirmed && r.Assumption == "" {
			errs = append(errs, fmt.Errorf("device %q: unconfirmed ports need an assumption", r.ID))
		}
		d := &DeviceModel{ID: r.ID, Name: r.Name, Aliases: r.Aliases, Confirmed: r.Confirmed, Assumption: r.Assumption}
		for _, rp := range r.Ports {
			p, err := c.newPort(rp)
			if err != nil {
				errs = append(errs, fmt.Errorf("device %q port %q: %w", r.ID, rp.Label, err))
				continue
			}
			d.Ports = append(d.Ports, p)
		}
		c.Devices[d.ID] = d
		c.deviceOrder = append(c.deviceOrder, d)
	}
	return errs
}

func (c *Catalog) newPort(r rawPort) (Port, error) {
	e, err := ParseEnd(r.End)
	if err != nil {
		return Port{}, err
	}
	if _, ok := c.Connectors[e.Connector]; !ok {
		return Port{}, fmt.Errorf("unknown connector %q", e.Connector)
	}
	if r.Dir != "in" && r.Dir != "out" && r.Dir != "both" {
		return Port{}, fmt.Errorf("dir must be in, out or both, not %q", r.Dir)
	}
	if len(r.Signals) == 0 {
		return Port{}, fmt.Errorf("no signals")
	}
	for _, s := range r.Signals {
		if _, ok := c.Signals[s]; !ok {
			return Port{}, fmt.Errorf("unknown signal %q", s)
		}
	}
	for _, s := range r.OutOnly {
		if !slices.Contains(r.Signals, s) {
			return Port{}, fmt.Errorf("out_only signal %q is not one of its signals", s)
		}
	}
	if r.Qty == 0 {
		r.Qty = 1
	}
	return Port{Label: r.Label, End: e, Qty: r.Qty, Dir: r.Dir, Signals: r.Signals, OutOnly: r.OutOnly, Volts: r.Volts, Note: r.Note}, nil
}

// Fit is a cable or adapter type that plugs into a port.
type Fit struct {
	Part         *Part
	End          End      // the part's end that goes into the port
	Signals      []string // the port's signals this part can carry
	WrongVoltage bool     // a power supply rated for another voltage
}

// Fits lists every catalog part with an end that plugs into p and carries at
// least one of its signals. An end with fewer plugs than the port still fits
// (red/white RCA into a yellow/white/red input carries the audio), and so does
// a mating connector (a USB 3.0 Type-A plug in a USB-A socket).
func (c *Catalog) Fits(p Port) []Fit {
	connectors := []string{p.End.Connector}
	if cn, ok := c.Connectors[p.End.Connector]; ok {
		connectors = append(connectors, cn.Mates...)
	}
	var out []Fit
	seen := map[string]bool{}
	for _, conn := range connectors {
		for _, part := range c.ByConnector[conn] {
			if seen[part.Key] {
				continue
			}
			for _, e := range part.Ends {
				if e.Connector != conn || e.Gender == p.End.Gender || e.Count > p.End.Count {
					continue
				}
				var shared []string
				for _, s := range p.Signals {
					if slices.Contains(part.Signals, s) {
						shared = append(shared, s)
					}
				}
				if len(shared) == 0 {
					continue
				}
				seen[part.Key] = true
				out = append(out, Fit{
					Part:         part,
					End:          e,
					Signals:      shared,
					WrongVoltage: p.Volts > 0 && part.Volts > 0 && p.Volts != part.Volts,
				})
				break
			}
		}
	}
	return out
}

// DeviceModels returns every model in catalog order.
func (c *Catalog) DeviceModels() []*DeviceModel { return c.deviceOrder }
