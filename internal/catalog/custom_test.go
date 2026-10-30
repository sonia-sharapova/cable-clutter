package catalog_test

import (
	"strings"
	"testing"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
)

func TestCustomEntriesLoad(t *testing.T) {
	custom := catalog.Custom{
		Cables: []catalog.CustomPart{{Name: "Odd sync cable", Ends: []string{"usb-a:m", "rca*3:m"}, Variant: "mine", Signals: []string{"video.composite"}}},
		Devices: []catalog.CustomDevice{{ID: "custom-gizmo", Name: "Gizmo", Ports: []catalog.CustomPort{
			{Label: "AV out", End: "rca*3:f", Dir: "out", Signals: []string{"video.composite", "audio.analog.stereo"}},
		}}},
	}
	c, err := catalog.LoadCompiled(catalogdata.Compiled, "test", custom)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Part("rca*3:m|usb-a:m#mine"); !ok {
		t.Error("custom cable missing")
	}
	if d := c.Devices["custom-gizmo"]; d == nil || len(d.Ports) != 1 || !d.Confirmed {
		t.Errorf("custom device: %+v", d)
	}
}

func TestCheckCustom(t *testing.T) {
	c, err := catalog.LoadCompiled(catalogdata.Compiled, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CheckPart(catalog.KindCable, catalog.CustomPart{Name: "x", Ends: []string{"usb-a:m"}, Signals: []string{"usb.2.0"}}); err == nil {
		t.Error("one end should be refused")
	}
	if _, err := c.CheckPart(catalog.KindCable, catalog.CustomPart{Name: "x", Ends: []string{"nope:m", "usb-a:m"}, Signals: []string{"usb.2.0"}}); err == nil {
		t.Error("unknown connector should be refused")
	}
	var existing *catalog.Part
	for _, p := range c.Cables {
		existing = p
		break
	}
	ends := []string{}
	for _, e := range existing.Ends {
		ends = append(ends, e.String())
	}
	_, err = c.CheckPart(catalog.KindCable, catalog.CustomPart{Name: "dup", Ends: ends, Variant: existing.Variant, Signals: existing.Signals})
	if err == nil || !strings.Contains(err.Error(), "already has") {
		t.Errorf("duplicate ends: %v", err)
	}
	if err := c.CheckDevice(catalog.CustomDevice{ID: "custom-x", Name: "X", Ports: []catalog.CustomPort{{Label: "p", End: "usb-c:f", Dir: "sideways", Signals: []string{"usb.2.0"}}}}); err == nil {
		t.Error("bad direction should be refused")
	}
}
