package smart_test

import (
	"path/filepath"
	"slices"
	"testing"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/route"
	"github.com/sonia-sharapova/cable-clutter/internal/smart"
)

type world struct {
	c       *catalog.Catalog
	db      *library.DB
	devices []smart.Device
}

func setup(t *testing.T, models map[string]string, parts ...string) *world {
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
	for nick, model := range models {
		db.AddDevice(model, nick, "")
	}
	for _, k := range parts {
		p, ok := c.Part(k)
		if !ok {
			t.Fatalf("no part %s", k)
		}
		db.AddItems(library.Item{Kind: p.Kind, PartKey: k}, 1)
	}
	ds, _ := db.Devices()
	return &world{c, db, smart.Devices(c, ds)}
}

func (w *world) inv() plan.Inventories {
	cables, _ := w.db.LoadInventory(catalog.KindCable)
	adapters, _ := w.db.LoadInventory(catalog.KindAdapter)
	return plan.Inventories{Cables: cables, Adapters: adapters}
}

func (w *world) device(nick string) smart.Device {
	for _, d := range w.devices {
		if d.Nickname == nick {
			return d
		}
	}
	panic(nick)
}

func TestReach(t *testing.T) {
	w := setup(t, map[string]string{"PS2": "ps2-slim", "CRT": "crt-tv", "iPod": "ipod-2nd-gen"}, "ps2-avmulti:m|rca*3:m")
	reach := smart.ReachFrom(w.c, w.device("PS2"), w.devices, w.inv())
	if len(reach) != 1 || reach[0].Other.Nickname != "CRT" || reach[0].Outgoing["video"] == nil || len(reach[0].Incoming) != 0 {
		t.Fatalf("reach = %+v", reach)
	}
}

func TestChainNeverReusesAnItem(t *testing.T) {
	// One aux cable, two hops that each want one.
	w := setup(t, map[string]string{"iPhone": "iphone-5", "Mac": "mac-mini-m2-2023", "Speaker": "te-ob-4"}, "trs-3.5:m|trs-3.5:m")
	hops := smart.Chain(w.c, []smart.Device{w.device("iPhone"), w.device("Speaker")}, w.inv(), route.Options{})
	if r := hops[0].Routes["audio"]; r == nil || r.Steps[0].Item == nil {
		t.Fatalf("single hop should use the owned aux cable: %+v", hops[0].Routes)
	}
	hops = smart.Chain(w.c, []smart.Device{w.device("Mac"), w.device("iPhone"), w.device("Speaker")}, w.inv(), route.Options{})
	used := map[string]int{}
	for _, h := range hops {
		for _, r := range h.Routes {
			for _, s := range r.Steps {
				if s.Item != nil {
					used[s.Item.ID]++
				}
			}
		}
	}
	for id, n := range used {
		if n > 1 {
			t.Errorf("item %s used %d times across hops", id, n)
		}
	}
}

func TestChainSharesOneCableAcrossSignals(t *testing.T) {
	w := setup(t, map[string]string{"PS2": "ps2-slim", "CRT": "crt-tv"}, "ps2-avmulti:m|rca*3:m")
	hops := smart.Chain(w.c, []smart.Device{w.device("PS2"), w.device("CRT")}, w.inv(), route.Options{})
	v, a := hops[0].Routes["video"], hops[0].Routes["audio"]
	if v == nil || a == nil || !v.SameSteps(a) || v.Steps[0].Item == nil {
		t.Errorf("the AV cable should carry both video and audio: %+v", hops[0].Routes)
	}
}

func TestBestBuy(t *testing.T) {
	w := setup(t, map[string]string{"Mac": "mac-mini-m2-2023", "iPod": "ipod-2nd-gen", "PS2": "ps2-slim"})
	unlocks := smart.BestBuy(w.c, w.devices, w.inv())
	i := slices.IndexFunc(unlocks, func(u smart.Unlock) bool { return u.Part.Key == "apple-30pin:m|usb-a:m" })
	if i < 0 {
		t.Fatalf("a USB-A to 30-pin cable should unlock the iPod: %v", unlocks)
	}
	if n := len(unlocks[i].Pairs); n != 4 { // iPod ↔ Mac and iPod ↔ PS2, both ways
		t.Errorf("30-pin cable unlocks %d connections, want 4: %v", n, unlocks[i].Pairs)
	}
	for j := 1; j < len(unlocks); j++ {
		if len(unlocks[j].Pairs) > len(unlocks[j-1].Pairs) {
			t.Fatal("not ranked by connections unlocked")
		}
	}
}

func TestUnused(t *testing.T) {
	w := setup(t, map[string]string{"PS2": "ps2-slim", "PSP": "psp"},
		"dc-barrel:m|nema-1-15:m#8.5v", // PS2 adaptor: useful
		"cee-7-16:m|nema-1-15:f",       // US to EU: plugs into the PS2 adaptor's free end
		"dc-barrel:m|nema-5-15:m#9v",   // wrong voltage for both: unused
		"nema-1-15:m|nema-5-15:f",      // 3 to 2 prong: only joins the 9 V adaptor
		"mini-usb:m|usb-a:m",           // PSP cable
		"nema-1-15:m|usb-a:f",          // wall charger: the PSP cable plugs into it
		"esata:f|sata:f",               // fits nothing
	)
	got := smart.Unused(w.c, w.devices, w.inv())
	want := []string{"dc-barrel:m|nema-5-15:m#9v", "esata:f|sata:f", "nema-1-15:m|nema-5-15:f"}
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, want) {
		t.Errorf("unused = %v, want %v", keys, want)
	}
}
