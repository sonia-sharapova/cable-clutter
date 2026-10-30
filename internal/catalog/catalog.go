// Package catalog loads the connector catalog and builds the in-memory
// hashmap indexes every query runs against.
package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"gopkg.in/yaml.v3"
)

type Signal struct {
	ID   string
	Kind string
	Name string
}

type Connector struct {
	ID      string
	Family  string
	Name    string
	Aliases []string
	Mates   []string // other connectors it physically plugs into
}

type Kind string

const (
	KindCable   Kind = "cable"
	KindAdapter Kind = "adapter"
)

// Part is a cable type or an adapter type.
type Part struct {
	Key     string
	Kind    Kind
	Name    string
	Ends    []End
	Variant string
	Signals []string
	Active  bool   // contains electronics (converter, DAC, hub)
	Source  string // for one-way converters: the connector audio/video enters by
	// InSignals, for converters that change the signal: what they accept at
	// Source. What comes out is Signals.
	InSignals []string
	Volts     float64
	Amps      float64
	Aliases   []string
	Note      string
}

// Catalog is the read-only catalog plus its indexes. Every map lookup is O(1);
// nothing here touches disk after Load returns.
type Catalog struct {
	Version     string
	Signals     map[string]*Signal
	Connectors  map[string]*Connector
	Cables      map[string]*Part // canonical key -> cable type
	Adapters    map[string]*Part // canonical key -> adapter type
	ByConnector map[string][]*Part
	BySignal    map[string][]*Part
	Devices     map[string]*DeviceModel

	deviceOrder []*DeviceModel

	glyphs  map[string]Glyph // connector id, "family:<name>" or "default"
	aliases map[string][]Ref // normalised name -> what it names
	terms   []term           // sorted, for prefix search
}

type rawPart struct {
	Ends      []string
	Variant   string
	Signals   []string
	Active    bool
	Source    string
	InSignals []string `yaml:"in_signals"`
	Volts     float64
	Amps      float64
	Name      string
	Aliases   []string
	Note      string
}

type rawGlyph struct {
	Fill   string
	Art    string
	Female string
}

type rawFile struct {
	Signals    []Signal
	Connectors []Connector
	Cables     []rawPart
	Adapters   []rawPart
	Glyphs     map[string]rawGlyph
	Devices    []rawDevice
}

// Load reads every *.yaml file in fsys, validates cross-references and builds
// the indexes. Any invalid entry fails the whole load, so a bad catalog
// never ships.
func Load(fsys fs.FS, version string) (*Catalog, error) {
	files, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	var all rawFile
	for _, name := range files {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var f rawFile
		if err := yaml.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", path.Base(name), err)
		}
		all.Signals = append(all.Signals, f.Signals...)
		all.Connectors = append(all.Connectors, f.Connectors...)
		all.Cables = append(all.Cables, f.Cables...)
		all.Adapters = append(all.Adapters, f.Adapters...)
		all.Devices = append(all.Devices, f.Devices...)
		for k, g := range f.Glyphs {
			if all.Glyphs == nil {
				all.Glyphs = map[string]rawGlyph{}
			}
			all.Glyphs[k] = g
		}
	}
	return build(all, version)
}

func build(raw rawFile, version string) (*Catalog, error) {
	c := &Catalog{
		Version:     version,
		Signals:     make(map[string]*Signal, len(raw.Signals)),
		Connectors:  make(map[string]*Connector, len(raw.Connectors)),
		Cables:      make(map[string]*Part, len(raw.Cables)),
		Adapters:    make(map[string]*Part, len(raw.Adapters)),
		ByConnector: make(map[string][]*Part),
		BySignal:    make(map[string][]*Part),
		Devices:     make(map[string]*DeviceModel),
		glyphs:      make(map[string]Glyph),
		aliases:     make(map[string][]Ref),
	}
	var errs []error

	for i := range raw.Signals {
		s := &raw.Signals[i]
		if _, dup := c.Signals[s.ID]; dup {
			errs = append(errs, fmt.Errorf("signal %q defined twice", s.ID))
		}
		c.Signals[s.ID] = s
	}
	for i := range raw.Connectors {
		cn := &raw.Connectors[i]
		if _, dup := c.Connectors[cn.ID]; dup {
			errs = append(errs, fmt.Errorf("connector %q defined twice", cn.ID))
		}
		c.Connectors[cn.ID] = cn
	}
	// Mating is symmetric: if A's plug fits B's socket, B's fits A's.
	for _, cn := range c.Connectors {
		for _, m := range cn.Mates {
			other, ok := c.Connectors[m]
			if !ok {
				errs = append(errs, fmt.Errorf("connector %q mates with unknown %q", cn.ID, m))
				continue
			}
			if !slices.Contains(other.Mates, cn.ID) {
				other.Mates = append(other.Mates, cn.ID)
			}
		}
	}

	var ordered []*Part // YAML order, so index slices are deterministic
	addParts := func(kind Kind, raws []rawPart, into map[string]*Part) {
		for _, r := range raws {
			p, err := c.newPart(kind, r)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if prev, dup := into[p.Key]; dup {
				errs = append(errs, fmt.Errorf("%s %q and %q share key %s; add a variant", kind, prev.Name, p.Name, p.Key))
				continue
			}
			into[p.Key] = p
			ordered = append(ordered, p)
		}
	}
	addParts(KindCable, raw.Cables, c.Cables)
	addParts(KindAdapter, raw.Adapters, c.Adapters)
	errs = append(errs, c.addDevices(raw.Devices)...)
	if err := c.addGlyphs(raw.Glyphs); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	for _, p := range ordered {
		seen := map[string]bool{}
		for _, e := range p.Ends {
			if !seen[e.Connector] {
				seen[e.Connector] = true
				c.ByConnector[e.Connector] = append(c.ByConnector[e.Connector], p)
			}
		}
		for _, s := range p.Signals {
			c.BySignal[s] = append(c.BySignal[s], p)
		}
	}
	c.buildSearch(raw.Signals, raw.Connectors, ordered)
	return c, nil
}

func (c *Catalog) newPart(kind Kind, r rawPart) (*Part, error) {
	if r.Name == "" {
		return nil, fmt.Errorf("%s with ends %v has no name", kind, r.Ends)
	}
	if len(r.Ends) < 2 {
		return nil, fmt.Errorf("%s %q: needs at least 2 ends", kind, r.Name)
	}
	p := &Part{
		Kind: kind, Name: r.Name, Variant: r.Variant, Signals: r.Signals,
		Active: r.Active, Source: r.Source, InSignals: r.InSignals, Volts: r.Volts, Amps: r.Amps, Aliases: r.Aliases, Note: r.Note,
	}
	for _, s := range r.Ends {
		e, err := ParseEnd(s)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", kind, r.Name, err)
		}
		if _, ok := c.Connectors[e.Connector]; !ok {
			return nil, fmt.Errorf("%s %q: unknown connector %q", kind, r.Name, e.Connector)
		}
		p.Ends = append(p.Ends, e)
	}
	if len(p.Signals) == 0 {
		return nil, fmt.Errorf("%s %q: no signals", kind, r.Name)
	}
	for _, s := range p.Signals {
		if _, ok := c.Signals[s]; !ok {
			return nil, fmt.Errorf("%s %q: unknown signal %q", kind, r.Name, s)
		}
	}
	for _, s := range p.InSignals {
		if _, ok := c.Signals[s]; !ok {
			return nil, fmt.Errorf("%s %q: unknown in_signal %q", kind, r.Name, s)
		}
	}
	if len(p.InSignals) > 0 && p.Source == "" {
		return nil, fmt.Errorf("%s %q: in_signals needs a source end", kind, r.Name)
	}
	if p.Source != "" && !slices.ContainsFunc(p.Ends, func(e End) bool { return e.Connector == p.Source }) {
		return nil, fmt.Errorf("%s %q: source %q is not one of its ends", kind, r.Name, p.Source)
	}
	p.Key = Key(p.Ends, p.Variant)
	return p, nil
}

// Part returns the cable or adapter type with this canonical key.
func (c *Catalog) Part(key string) (*Part, bool) {
	if p, ok := c.Cables[key]; ok {
		return p, true
	}
	p, ok := c.Adapters[key]
	return p, ok
}
