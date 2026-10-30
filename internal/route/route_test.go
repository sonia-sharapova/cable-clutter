package route_test

import (
	"path/filepath"
	"testing"

	catalogdata "cable-clutter/catalog"
	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
	"cable-clutter/internal/plan"
	"cable-clutter/internal/route"
)

type fixture struct {
	c  *catalog.Catalog
	db *library.DB
}

func setup(t *testing.T) *fixture {
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
	return &fixture{c, db}
}

func (f *fixture) own(t *testing.T, key string) {
	t.Helper()
	p, ok := f.c.Part(key)
	if !ok {
		t.Fatalf("no part %s", key)
	}
	if _, err := f.db.AddItems(library.Item{Kind: p.Kind, PartKey: key}, 1); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) connect(t *testing.T, a, b string, opt route.Options) map[string]*route.Route {
	t.Helper()
	cables, _ := f.db.LoadInventory(catalog.KindCable)
	adapters, _ := f.db.LoadInventory(catalog.KindAdapter)
	return route.Connect(f.c, f.c.Devices[a], f.c.Devices[b], plan.Inventories{Cables: cables, Adapters: adapters}, opt)
}

func keys(r *route.Route) []string {
	var out []string
	for _, s := range r.Steps {
		k := s.Part.Key
		if s.Item == nil {
			k = "BUY " + k
		}
		out = append(out, k)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPS2ToCRTWithOwnedCable(t *testing.T) {
	f := setup(t)
	f.own(t, "ps2-avmulti:m|rca*3:m")
	routes := f.connect(t, "ps2-slim", "crt-tv", route.Options{})
	for _, kind := range []string{"video", "audio"} {
		r := routes[kind]
		if r == nil || !equal(keys(r), []string{"ps2-avmulti:m|rca*3:m"}) || r.Cost != route.CostFree {
			t.Errorf("%s route = %v", kind, r)
		}
	}
	if !routes["video"].SameSteps(routes["audio"]) {
		t.Error("video and audio should share the one AV cable")
	}
}

func TestSuggestsPurchaseWhenNothingOwned(t *testing.T) {
	f := setup(t)
	r := f.connect(t, "raspberry-pi-3b", "crt-tv", route.Options{})["video"]
	if r == nil || !equal(keys(r), []string{"BUY rca*3:m|trrs-3.5:m"}) {
		t.Fatalf("Pi → CRT video = %v", r)
	}
	if len(r.Buy()) != 1 {
		t.Errorf("Buy() = %v", r.Buy())
	}
	if got := f.connect(t, "raspberry-pi-3b", "crt-tv", route.Options{OwnedOnly: true}); len(got) != 0 {
		t.Errorf("--owned with nothing owned found %d routes", len(got))
	}
}

func TestChainsAndConverts(t *testing.T) {
	f := setup(t)
	f.own(t, "hdmi:m|hdmi:m")
	f.own(t, "rca*3:m|rca*3:m")
	f.own(t, "hdmi:f|rca*3:f") // HDMI to composite converter
	r := f.connect(t, "mac-mini-m2-2023", "crt-tv", route.Options{})["video"]
	want := []string{"hdmi:m|hdmi:m", "hdmi:f|rca*3:f", "rca*3:m|rca*3:m"}
	if r == nil || !equal(keys(r), want) || r.Signal != "video.composite" {
		t.Fatalf("Mac mini → CRT video = %v, want %v as composite", r, want)
	}
	// Two steps short: no route.
	if r := f.connect(t, "mac-mini-m2-2023", "crt-tv", route.Options{MaxSteps: 2})["video"]; r != nil && len(r.Buy()) == 0 {
		t.Errorf("found an owned 2-step route: %v", keys(r))
	}
}

func TestDirection(t *testing.T) {
	f := setup(t)
	f.own(t, "hdmi:m|usb-c:m")                 // USB-C to HDMI: video from USB-C only
	f.own(t, "hdmi:f|lightning:f|lightning:m") // Lightning AV adapter
	f.own(t, "hdmi:m|hdmi:m")

	if r := f.connect(t, "macbook-pro-13-m1-2020", "dell-monitor", route.Options{})["video"]; r == nil || !equal(keys(r), []string{"hdmi:m|usb-c:m"}) {
		t.Errorf("MacBook → Dell video = %v", r)
	}
	// A laptop's USB-C port sends video but never takes it in, and the
	// USB-C to HDMI cable doesn't run backwards.
	if r := f.connect(t, "iphone-5", "macbook-pro-13-m1-2020", route.Options{})["video"]; r != nil {
		t.Errorf("iPhone → MacBook video should be impossible, got %v", keys(r))
	}
	if r := f.connect(t, "dell-monitor", "macbook-pro-13-m1-2020", route.Options{})["video"]; r != nil {
		t.Errorf("Dell → MacBook video should be impossible, got %v", keys(r))
	}
	if r := f.connect(t, "iphone-5", "dell-monitor", route.Options{})["video"]; r == nil || !equal(keys(r), []string{"hdmi:f|lightning:f|lightning:m", "hdmi:m|hdmi:m"}) {
		t.Errorf("iPhone → Dell video = %v", r)
	}
}

func TestPrefersFreeOverInUse(t *testing.T) {
	f := setup(t)
	f.own(t, "trs-3.5:m|trs-3.5:m")
	cables, _ := f.db.LoadInventory(catalog.KindCable)
	aux := cables.Order[0]
	s, _ := f.db.NewSystem("Desk", "")
	l, _ := f.db.AddLink(library.Link{SystemID: s.ID, PartKey: aux.PartKey, Preferred: aux.ID})
	f.db.BringUp(s.ID, map[string]string{l.ID: aux.ID})

	r := f.connect(t, "iphone-5", "te-ob-4", route.Options{})["audio"]
	if r == nil || r.Steps[0].Busy != "Desk" || r.Cost != route.CostInUse {
		t.Fatalf("with the only aux cable busy: %+v", r)
	}
	f.own(t, "trs-3.5:m|trs-3.5:m")
	r = f.connect(t, "iphone-5", "te-ob-4", route.Options{})["audio"]
	if r.Steps[0].Busy != "" || r.Cost != route.CostFree {
		t.Errorf("a free aux cable exists but the route uses the busy one")
	}
}

// Data plan budget: the resolver answers in under 20 ms.
func BenchmarkConnect(b *testing.B) {
	f := setup(&testing.T{})
	cables, _ := f.db.LoadInventory(catalog.KindCable)
	adapters, _ := f.db.LoadInventory(catalog.KindAdapter)
	inv := plan.Inventories{Cables: cables, Adapters: adapters}
	mac, crt := f.c.Devices["mac-mini-m2-2023"], f.c.Devices["crt-tv"]
	for b.Loop() {
		route.Connect(f.c, mac, crt, inv, route.Options{})
	}
}
