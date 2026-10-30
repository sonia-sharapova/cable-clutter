package catalog_test

import (
	"testing"

	"cable-clutter/internal/catalog"
)

func TestResolve(t *testing.T) {
	c := load(t)
	for _, tc := range []struct {
		kind    catalog.Kind
		args    []string
		variant string
		want    string
	}{
		{catalog.KindCable, []string{"usb-c", "usb-a"}, "", "usb-a:m|usb-c:m"},
		{catalog.KindCable, []string{"aux", "aux"}, "", "trs-3.5:m|trs-3.5:m"}, // not the splitter
		{catalog.KindCable, []string{"aux", "rca"}, "", "rca*2:m|trs-3.5:m"},   // any count when 1 doesn't fit
		{catalog.KindCable, []string{"usbs", "lightning"}, "", "lightning:m|usb-a-3.0:m"},
		{catalog.KindCable, []string{"2-prong", "barrel"}, "8.5V", "dc-barrel:m|nema-1-15:m#8.5v"},
		{catalog.KindCable, []string{"aux", "cable"}, "", "trs-3.5:m|trs-3.5:m"}, // by name
		{catalog.KindCable, []string{"usb-a:m|usb-c:m"}, "", "usb-a:m|usb-c:m"},  // by key
		{catalog.KindAdapter, []string{"usb-c hub with card reader"}, "", "hdmi:f|microsd:f|sd:f|usb-a-3.0:f|usb-a:f|usb-c:f|usb-c:m"},
		{catalog.KindAdapter, []string{"nema-1-15:f", "cee-7-16:m"}, "", "cee-7-16:m|nema-1-15:f"},
	} {
		p, cands := c.Resolve(tc.kind, tc.args, tc.variant)
		if p == nil {
			t.Errorf("Resolve(%v, %q) = %d candidates, want %s", tc.args, tc.variant, len(cands), tc.want)
			continue
		}
		if p.Key != tc.want {
			t.Errorf("Resolve(%v, %q) = %s, want %s", tc.args, tc.variant, p.Key, tc.want)
		}
	}
}

func TestResolveAmbiguousOrUnknown(t *testing.T) {
	c := load(t)
	// Several power supplies share these ends; without a variant it's ambiguous.
	if p, cands := c.Resolve(catalog.KindCable, []string{"2-prong", "barrel"}, ""); p != nil || len(cands) < 3 {
		t.Errorf("2-prong barrel: got %v and %d candidates, want ambiguity", p, len(cands))
	}
	// Cables never match adapter types.
	if p, cands := c.Resolve(catalog.KindCable, []string{"usb-c hub with card reader"}, ""); p != nil || len(cands) != 0 {
		t.Errorf("hub as a cable: got %v, %d candidates", p, len(cands))
	}
	if p, cands := c.Resolve(catalog.KindCable, []string{"toaster", "usb-c"}, ""); p != nil || len(cands) != 0 {
		t.Errorf("unknown connector: got %v, %d candidates", p, len(cands))
	}
}
