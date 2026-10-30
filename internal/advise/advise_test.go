package advise_test

import (
	"path/filepath"
	"slices"
	"testing"

	catalogdata "cable-clutter/catalog"
	"cable-clutter/internal/advise"
	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
)

type fixture struct {
	c                *catalog.Catalog
	db               *library.DB
	cables, adapters *library.Inventory
}

// setup loads the catalog and a library holding one of each given cable key.
func setup(t *testing.T, cableKeys ...string) *fixture {
	t.Helper()
	c, err := catalog.Load(catalogdata.FS, catalogdata.Version)
	if err != nil {
		t.Fatal(err)
	}
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, k := range cableKeys {
		if _, ok := c.Cables[k]; !ok {
			t.Fatalf("no cable %s", k)
		}
		if _, err := db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: k}, 1); err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{c: c, db: db}
	f.cables, _ = db.LoadInventory(catalog.KindCable)
	f.adapters, _ = db.LoadInventory(catalog.KindAdapter)
	return f
}

func (f *fixture) port(t *testing.T, model, label string) advise.PortReport {
	t.Helper()
	m := f.c.Devices[model]
	for _, r := range advise.Device(f.c, m, f.cables, f.adapters, "") {
		if r.Port.Label == label {
			return r
		}
	}
	t.Fatalf("%s has no port %q", model, label)
	return advise.PortReport{}
}

func names(owned []advise.Owned) []string {
	var out []string
	for _, o := range owned {
		out = append(out, o.Fit.Part.Key)
	}
	return out
}

func TestPS2AVOut(t *testing.T) {
	f := setup(t, "ps2-avmulti:m|rca*3:m")
	r := f.port(t, "ps2-slim", "AV Multi Out")
	if !slices.Equal(r.Can, []string{"video.composite", "audio.analog.stereo"}) {
		t.Errorf("Can = %v", r.Can)
	}
	var sugg []string
	for _, s := range r.CantYet {
		sugg = append(sugg, s.Part.Key)
	}
	if !slices.Contains(sugg, "ps2-avmulti:m|rca*5:m") || !slices.Contains(sugg, "ps2-avmulti:m|rca*2:m|s-video:m") {
		t.Errorf("CantYet = %v, want component and S-Video cables", sugg)
	}
}

func TestPowerVoltages(t *testing.T) {
	f := setup(t, "dc-barrel:m|nema-1-15:m#8.5v", "dc-barrel:m|nema-1-15:m#5v-2a", "dc-barrel:m|nema-1-15:m#unknown")
	r := f.port(t, "ps2-slim", "DC in")
	if got := names(r.Have); !slices.Equal(got, []string{"dc-barrel:m|nema-1-15:m#8.5v"}) {
		t.Errorf("Have = %v, want only the 8.5 V adaptor", got)
	}
	if got := names(r.Wrong); !slices.Equal(got, []string{"dc-barrel:m|nema-1-15:m#5v-2a"}) {
		t.Errorf("Wrong = %v, want the 5 V adaptor", got)
	}
	if got := names(r.Unsure); !slices.Equal(got, []string{"dc-barrel:m|nema-1-15:m#unknown"}) {
		t.Errorf("Unsure = %v, want the unknown adaptor", got)
	}
	// Never suggest a power supply of the wrong or unknown voltage.
	r = f.port(t, "nintendo-3ds", "Charge")
	for _, s := range r.CantYet {
		if v := s.Part.Volts; v != 0 && v != 4.6 {
			t.Errorf("suggested %s (%gV) for a 4.6V port", s.Part.Name, v)
		}
	}
}

func TestPoweredPortWantsNoOtherPower(t *testing.T) {
	f := setup(t, "dc-barrel:m|nema-1-15:m#5v-2a")
	r := f.port(t, "psp", "DC in")
	if len(r.CantYet) != 0 {
		t.Errorf("PSP DC in is powered, but suggests %v", r.CantYet[0].Part.Name)
	}
}

func TestPartialFit(t *testing.T) {
	// Red/white RCA into a yellow/white/red input carries the audio only.
	f := setup(t, "rca*2:m|trs-3.5:m")
	r := f.port(t, "crt-tv", "AV in")
	if !slices.Equal(r.Can, []string{"audio.analog.stereo"}) {
		t.Errorf("Can = %v, want stereo audio only", r.Can)
	}
	if len(r.CantYet) == 0 || !slices.Contains(r.CantYet[0].Signals, "video.composite") {
		t.Errorf("CantYet = %v, want a composite video suggestion", r.CantYet)
	}
}

func TestMatingConnectors(t *testing.T) {
	// A USB 3.0 Type-A plug fits the PS2's USB-A sockets.
	f := setup(t, "lightning:m|usb-a-3.0:m")
	if got := names(f.port(t, "ps2-slim", "USB").Have); !slices.Equal(got, []string{"lightning:m|usb-a-3.0:m"}) {
		t.Errorf("Have = %v", got)
	}
	// The Pi's TRRS AV jack takes a plain aux plug for audio, and the
	// suggestion for composite video is the TRRS to RCA cable.
	f = setup(t, "trs-3.5:m|trs-3.5:m")
	r := f.port(t, "raspberry-pi-3b", "AV out")
	if !slices.Equal(r.Can, []string{"audio.analog.stereo"}) {
		t.Errorf("Pi AV Can = %v", r.Can)
	}
	if len(r.CantYet) != 1 || r.CantYet[0].Part.Key != "rca*3:m|trrs-3.5:m" {
		t.Errorf("Pi AV CantYet = %v", r.CantYet)
	}
}

func TestEveryDeviceReports(t *testing.T) {
	f := setup(t)
	for _, m := range f.c.DeviceModels() {
		reports := advise.Device(f.c, m, f.cables, f.adapters, "")
		if len(reports) != len(m.Ports) {
			t.Errorf("%s: %d reports for %d ports", m.ID, len(reports), len(m.Ports))
		}
		for _, r := range reports {
			if len(r.Have) != 0 {
				t.Errorf("%s %s: empty inventory but Have = %v", m.ID, r.Port.Label, names(r.Have))
			}
		}
	}
}
