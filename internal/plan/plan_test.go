package plan_test

import (
	"path/filepath"
	"testing"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
)

const hdmi = "hdmi:m|hdmi:m"

type world struct {
	t  *testing.T
	c  *catalog.Catalog
	db *library.DB
}

func newWorld(t *testing.T) *world {
	c, err := catalog.Load(catalogdata.FS, catalogdata.Version)
	if err != nil {
		t.Fatal(err)
	}
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &world{t, c, db}
}

func (w *world) items(key string, n int, dedicated string) []*library.Item {
	its, err := w.db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: key, Dedicated: dedicated}, n)
	if err != nil {
		w.t.Fatal(err)
	}
	return its
}

func (w *world) system(name string, links ...library.Link) *library.System {
	s, err := w.db.NewSystem(name, "")
	if err != nil {
		w.t.Fatal(err)
	}
	for _, l := range links {
		l.SystemID = s.ID
		if _, err := w.db.AddLink(l); err != nil {
			w.t.Fatal(err)
		}
	}
	return s
}

func (w *world) inv() plan.Inventories {
	cables, _ := w.db.LoadInventory(catalog.KindCable)
	adapters, _ := w.db.LoadInventory(catalog.KindAdapter)
	return plan.Inventories{Cables: cables, Adapters: adapters}
}

func (w *world) links(s *library.System) []*library.Link {
	all, err := w.db.Links()
	if err != nil {
		w.t.Fatal(err)
	}
	return all[s.ID]
}

func (w *world) up(s *library.System) {
	picks, short := plan.Allocate(w.c, w.links(s), w.inv())
	if len(short) > 0 {
		w.t.Fatalf("%s: unexpected shortage", s.Name)
	}
	assigned := map[string]string{}
	for _, p := range picks {
		assigned[p.Link.ID] = p.Item.ID
	}
	if err := w.db.BringUp(s.ID, assigned); err != nil {
		w.t.Fatal(err)
	}
}

func TestAllocatePrefersPinnedThenDedicated(t *testing.T) {
	w := newWorld(t)
	spare := w.items(hdmi, 1, "")[0]
	mine := w.items(hdmi, 1, "Apple TV")[0]
	pinned := w.items(hdmi, 1, "")[0]

	s := w.system("TV", library.Link{PartKey: hdmi, Preferred: pinned.ID}, library.Link{PartKey: hdmi})
	picks, short := plan.Allocate(w.c, w.links(s), w.inv())
	if len(short) != 0 || picks[0].Item.ID != pinned.ID || picks[0].Swapped {
		t.Fatalf("link 1 should use its pinned item")
	}
	if picks[1].Item.ID != spare.ID {
		t.Errorf("link 2 took a dedicated cable over a spare")
	}

	// A link to the Apple TV prefers the cable dedicated to it.
	w.db.AddDevice("apple-tv-hd", "Apple TV", "")
	s2 := w.system("Apple TV", library.Link{PartKey: hdmi, FromDevice: "Apple TV"})
	picks, _ = plan.Allocate(w.c, w.links(s2), w.inv())
	if picks[0].Item.ID != mine.ID {
		t.Errorf("Apple TV link didn't use its dedicated cable")
	}
}

func TestAllocateSwapsAndReportsHolders(t *testing.T) {
	w := newWorld(t)
	its := w.items(hdmi, 2, "")
	a := w.system("A", library.Link{PartKey: hdmi, Preferred: its[0].ID})
	b := w.system("B", library.Link{PartKey: hdmi, Preferred: its[0].ID})
	w.up(a)

	picks, short := plan.Allocate(w.c, w.links(b), w.inv())
	if len(short) != 0 || picks[0].Item.ID != its[1].ID || !picks[0].Swapped {
		t.Fatalf("B should swap in the second HDMI cable")
	}
	w.up(b)

	c := w.system("C", library.Link{PartKey: hdmi})
	_, short = plan.Allocate(w.c, w.links(c), w.inv())
	if len(short) != 1 || short[0].Owned != 2 || len(short[0].HeldBy) != 2 || short[0].HeldBy[0] != "A" {
		t.Fatalf("shortage = %+v, want both cables held by A and B", short)
	}

	// Taking A down frees its cable for C.
	w.db.TakeDown(a.ID)
	if _, short = plan.Allocate(w.c, w.links(c), w.inv()); len(short) != 0 {
		t.Error("C still short after A went down")
	}
}

func TestConflictsAndShort(t *testing.T) {
	w := newWorld(t)
	w.items(hdmi, 1, "")
	for _, d := range [][2]string{{"mac-mini-m2-2023", "Mac mini"}, {"apple-tv-hd", "Apple TV"}, {"dell-monitor", "Dell"}, {"raspberry-pi-3b", "Pi"}} {
		w.db.AddDevice(d[0], d[1], "")
	}
	a := w.system("Desk", library.Link{PartKey: hdmi, FromDevice: "Mac mini", FromPort: "HDMI"})
	b := w.system("Lounge", library.Link{PartKey: hdmi, FromDevice: "Apple TV", FromPort: "HDMI"})
	twice := w.system("Twice", library.Link{PartKey: hdmi}, library.Link{PartKey: hdmi})

	devices, _ := w.db.Devices()
	ports := plan.NewPorts(w.c, devices)
	systems, _ := w.db.Systems()
	links, _ := w.db.Links()
	got := plan.Conflicts(w.c, ports, systems, links, w.inv())
	if len(got[a.ID]) != 1 || got[a.ID][0].Other.ID != b.ID || got[a.ID][0].PartKey != hdmi {
		t.Errorf("Desk conflicts = %+v, want Lounge over the one HDMI cable", got[a.ID])
	}
	if len(got[twice.ID]) != 0 {
		t.Errorf("Twice alone needs more than owned; it belongs in Short, not Conflicts")
	}
	if m := plan.Short(w.c, links[twice.ID], w.inv()); len(m) != 1 || m[0].Need != 2 || m[0].Owned != 1 {
		t.Errorf("Short(Twice) = %+v", m)
	}

	// With a second cable, the only clash left is the Dell's one HDMI input.
	w.items(hdmi, 1, "")
	p1 := w.system("P1", library.Link{PartKey: hdmi, FromDevice: "Pi", FromPort: "HDMI", ToDevice: "Dell", ToPort: "HDMI in"})
	p2 := w.system("P2", library.Link{PartKey: hdmi, FromDevice: "Mac mini", FromPort: "HDMI", ToDevice: "Dell", ToPort: "HDMI in"})
	systems, _ = w.db.Systems()
	links, _ = w.db.Links()
	got = plan.Conflicts(w.c, ports, systems, links, w.inv())
	var portClash bool
	for _, cf := range got[p1.ID] {
		portClash = portClash || (cf.Other.ID == p2.ID && cf.Port == "Dell · HDMI in" && cf.Have == 1)
	}
	if !portClash {
		t.Errorf("P1 conflicts = %+v, want a clash with P2 on Dell · HDMI in", got[p1.ID])
	}
	w.up(p1)
	systems, _ = w.db.Systems()
	if busy := plan.BusyPorts(ports, p2, systems, links); len(busy) != 1 || busy[0].HeldBy[0] != "P1" {
		t.Errorf("BusyPorts(P2) = %+v", busy)
	}
}
