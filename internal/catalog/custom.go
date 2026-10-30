package catalog

import "fmt"

// Custom is what you've added yourself: cable, adapter and device types the
// bundled catalog lacks. It's built in with the bundled catalog and passes
// the same checks, so routes, systems and search treat it the same.
type Custom struct {
	Cables   []CustomPart
	Adapters []CustomPart
	Devices  []CustomDevice
}

// CustomPart is a cable or adapter type, as the library stores it.
type CustomPart struct {
	Name    string   `json:"name"`
	Ends    []string `json:"ends"` // "connector[*count]:gender", e.g. "rca*3:m"
	Variant string   `json:"variant,omitempty"`
	Signals []string `json:"signals"`
	Active  bool     `json:"active,omitempty"`
	Volts   float64  `json:"volts,omitempty"`
	Amps    float64  `json:"amps,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// CustomDevice is a device model with its ports. Ports copied from a
// generic type carry an assumption saying so.
type CustomDevice struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Make       string       `json:"make,omitempty"`
	Category   string       `json:"category,omitempty"`
	Wireless   string       `json:"wireless,omitempty"`
	Assumption string       `json:"assumption,omitempty"`
	Ports      []CustomPort `json:"ports"`
}

// CustomPort is one kind of port on a custom device.
type CustomPort struct {
	Label   string   `json:"label"`
	End     string   `json:"end"`
	Qty     int      `json:"qty,omitempty"`
	Dir     string   `json:"dir"` // in, out or both
	Signals []string `json:"signals"`
	Volts   float64  `json:"volts,omitempty"`
}

func (p CustomPart) raw() rawPart {
	return rawPart{Name: p.Name, Ends: p.Ends, Variant: p.Variant, Signals: p.Signals, Active: p.Active, Volts: p.Volts, Amps: p.Amps, Note: p.Note}
}

func (d CustomDevice) raw() rawDevice {
	// Ports you listed from the device in front of you are confirmed;
	// ones taken from a generic type are an assumption.
	r := rawDevice{ID: d.ID, Name: d.Name, Make: d.Make, Category: d.Category, Wireless: d.Wireless,
		Confirmed: d.Assumption == "", Assumption: d.Assumption}
	for _, p := range d.Ports {
		r.Ports = append(r.Ports, rawPort{Label: p.Label, End: p.End, Qty: p.Qty, Dir: p.Dir, Signals: p.Signals, Volts: p.Volts})
	}
	return r
}

// with returns the raw catalog plus the custom entries.
func (raw rawFile) with(custom Custom) rawFile {
	for _, p := range custom.Cables {
		raw.Cables = append(raw.Cables, p.raw())
	}
	for _, p := range custom.Adapters {
		raw.Adapters = append(raw.Adapters, p.raw())
	}
	for _, d := range custom.Devices {
		raw.Devices = append(raw.Devices, d.raw())
	}
	return raw
}

// CheckPart validates a new cable or adapter type against the catalog and
// returns its canonical key. A type whose ends already exist is refused:
// add that one instead.
func (c *Catalog) CheckPart(kind Kind, p CustomPart) (string, error) {
	part, err := c.newPart(kind, p.raw())
	if err != nil {
		return "", err
	}
	if prev, ok := c.Part(part.Key); ok {
		return "", fmt.Errorf("the catalog already has %q with those ends; add it from the list instead", prev.Name)
	}
	return part.Key, nil
}

// CheckDevice validates a new device model.
func (c *Catalog) CheckDevice(d CustomDevice) error {
	if _, dup := c.Devices[d.ID]; dup {
		return fmt.Errorf("a device model %q already exists", d.ID)
	}
	if len(d.Ports) == 0 && d.Wireless == "" {
		return fmt.Errorf("device %q: give it at least one port, or say how it connects wirelessly", d.Name)
	}
	probe := &Catalog{Signals: c.Signals, Connectors: c.Connectors, Devices: map[string]*DeviceModel{}}
	if errs := probe.addDevices([]rawDevice{d.raw()}); len(errs) > 0 {
		return errs[0]
	}
	return nil
}
