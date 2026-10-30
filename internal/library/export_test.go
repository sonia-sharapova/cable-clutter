package library_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

func TestExportImportRoundTrip(t *testing.T) {
	src := open(t)
	cables, _ := src.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "hdmi:m|hdmi:m", Nickname: "grey", Dedicated: "TV"}, 2)
	src.AddItems(library.Item{Kind: catalog.KindAdapter, PartKey: "cee-7-16:m|nema-1-15:f"}, 1)
	src.AddDevice("apple-tv-hd", "TV", "")
	s, _ := src.NewSystem("Lounge", "")
	l, _ := src.AddLink(library.Link{SystemID: s.ID, PartKey: "hdmi:m|hdmi:m", Preferred: cables[0].ID, FromDevice: "TV", FromPort: "HDMI"})
	src.BringUp(s.ID, map[string]string{l.ID: cables[0].ID})
	src.AddWish("rca*2:m|trs-3.5:m", 1, s.ID, "")
	src.RemoveItem(catalog.KindCable, cables[1].ID) // deleted rows stay out

	e, err := src.Export("test")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(e)
	parsed, err := library.ParseExport(data)
	if err != nil {
		t.Fatal(err)
	}

	dst := open(t)
	counts, err := dst.Import(parsed, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"devices": 1, "cable_items": 1, "adapter_items": 1, "systems": 1, "links": 1, "wishlist": 1}
	for tbl, n := range want {
		if counts[tbl] != n {
			t.Errorf("imported %d %s, want %d", counts[tbl], tbl, n)
		}
	}
	inv, _ := dst.LoadInventory(catalog.KindCable)
	it := inv.Items[cables[0].ID]
	if it == nil || it.Nickname != "grey" || it.Dedicated != "TV" || inv.Status[it.ID] != library.InUse || inv.HeldBy[it.ID] != "Lounge" {
		t.Fatalf("restored cable = %+v, status %s", it, inv.Status[cables[0].ID])
	}
	if it.CreatedAt != cables[0].CreatedAt {
		t.Errorf("timestamps changed: %d → %d", cables[0].CreatedAt, it.CreatedAt)
	}

	if _, err := dst.Import(parsed, false); err == nil {
		t.Error("imported into a non-empty library without --replace")
	}
	if _, err := dst.Import(parsed, true); err != nil {
		t.Errorf("--replace: %v", err)
	}
	if inv, _ := dst.LoadInventory(catalog.KindCable); len(inv.Order) != 1 {
		t.Errorf("after replace: %d cables, want 1", len(inv.Order))
	}

	parsed.Schema = library.LatestVersion() + 1
	if _, err := open(t).Import(parsed, false); err == nil {
		t.Error("imported an export from a newer schema")
	}
	if _, err := library.ParseExport([]byte(`{"format":"something-else"}`)); err == nil {
		t.Error("parsed a foreign document")
	}
}

// An export from before migration 3 has no assigned_item column: it loads.
func TestImportOlderSchema(t *testing.T) {
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := `{"format":"cable-clutter-export","schema":2,"catalog":"old","tables":{
		"systems":[{"id":"s1","name":"Old","active":0,"notes":"","created_at":1,"updated_at":1,"rev":1,"deleted":0}],
		"links":[{"id":"l1","system_id":"s1","part_key":"hdmi:m|hdmi:m","from_port":"","to_port":"","created_at":1,"updated_at":1,"rev":1,"deleted":0}]}}`
	e, err := library.ParseExport([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Import(e, false); err != nil {
		t.Fatalf("older export: %v", err)
	}
	links, _ := db.Links()
	if len(links["s1"]) != 1 || links["s1"][0].Assigned != "" {
		t.Errorf("links = %+v", links["s1"])
	}
}
