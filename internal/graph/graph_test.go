package graph_test

import (
	"path/filepath"
	"strings"
	"testing"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/graph"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/smart"
)

func TestGraph(t *testing.T) {
	c, _ := catalog.Load(catalogdata.FS, catalogdata.Version)
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.AddDevice("ps2-slim", "PS2", "")
	db.AddDevice("crt-tv", "CRT", "")
	db.AddDevice("ipod-2nd-gen", "iPod", "")
	db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "ps2-avmulti:m|rca*3:m"}, 2)
	cables, _ := db.LoadInventory(catalog.KindCable)
	adapters, _ := db.LoadInventory(catalog.KindAdapter)
	ds, _ := db.Devices()
	g := graph.Build(c, smart.Devices(c, ds), plan.Inventories{Cables: cables, Adapters: adapters})

	m := g.Mermaid()
	for _, want := range []string{"flowchart LR", `["PS2"]`, `(("PlayStation AV Multi Out"))`, "== x2 ==="} {
		if !strings.Contains(m, want) {
			t.Errorf("mermaid missing %q:\n%s", want, m)
		}
	}
	if d := g.DOT(); !strings.Contains(d, `"dev:PS2" -- "conn:ps2-avmulti"`) || !strings.Contains(d, "style=bold") {
		t.Errorf("dot:\n%s", d)
	}
	if got := g.Islands(); len(got) != 1 || got[0] != "iPod" {
		t.Errorf("islands = %v, want [iPod]", got)
	}
	around := g.Around("dev:PS2")
	for _, n := range around.Nodes {
		if n.Label == "iPod" {
			t.Error("Around(PS2) includes the unconnected iPod")
		}
	}
}
