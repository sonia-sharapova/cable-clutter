package library_test

import (
	"path/filepath"
	"testing"

	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
)

func open(t *testing.T) *library.DB {
	t.Helper()
	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAddLoadRemove(t *testing.T) {
	db := open(t)
	added, err := db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "usb-a:m|usb-c:m", Nickname: "white"}, 3)
	if err != nil || len(added) != 3 {
		t.Fatalf("AddItems: %d items, %v", len(added), err)
	}
	if _, err := db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "ps2-avmulti:m|rca*3:m", Dedicated: "PS2"}, 1); err != nil {
		t.Fatal(err)
	}
	// The same device name (any case) reuses the device row.
	if _, err := db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "dc-barrel:m|nema-1-15:m#8.5v", Dedicated: "ps2"}, 1); err != nil {
		t.Fatal(err)
	}
	var devices int
	db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&devices)
	if devices != 1 {
		t.Errorf("want 1 device, got %d", devices)
	}

	inv, err := db.LoadInventory(catalog.KindCable)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Order) != 5 || len(inv.Owned["usb-a:m|usb-c:m"]) != 3 {
		t.Fatalf("inventory has %d items, %d USB-A to USB-C", len(inv.Order), len(inv.Owned["usb-a:m|usb-c:m"]))
	}
	if got := inv.Owned["ps2-avmulti:m|rca*3:m"][0].Dedicated; got != "PS2" {
		t.Errorf("dedicated device = %q, want PS2", got)
	}
	if got := inv.Lookup("WHITE"); len(got) != 3 {
		t.Errorf("Lookup by nickname found %d, want 3", len(got))
	}
	first := added[0]
	if got := inv.Lookup("#" + first.ShortID()); len(got) != 1 || got[0].ID != first.ID {
		t.Errorf("Lookup by short id failed")
	}

	if err := db.RemoveItem(catalog.KindCable, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveItem(catalog.KindCable, first.ID); err == nil {
		t.Error("removing twice succeeded")
	}
	inv, _ = db.LoadInventory(catalog.KindCable)
	if _, ok := inv.Items[first.ID]; ok {
		t.Error("removed item still listed")
	}
	// Adapters live in their own inventory.
	if inv, _ := db.LoadInventory(catalog.KindAdapter); len(inv.Order) != 0 {
		t.Errorf("adapter inventory has %d items", len(inv.Order))
	}

	var changes int
	db.QueryRow(`SELECT COUNT(*) FROM changes`).Scan(&changes)
	if changes != 7 { // 5 items + 1 device + 1 delete
		t.Errorf("change log has %d rows, want 7", changes)
	}
}

func TestStatusIsDerivedFromSystems(t *testing.T) {
	db := open(t)
	added, _ := db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "hdmi:m|hdmi:m"}, 3)
	shared, solo, spare := added[0].ID, added[1].ID, added[2].ID
	for _, s := range []string{
		`INSERT INTO systems (id, name, active, created_at, updated_at) VALUES ('tv', 'TV', 1, 1, 1), ('desk', 'Desk', 0, 1, 1)`,
		`INSERT INTO links (id, system_id, part_key, preferred_item, assigned_item, created_at, updated_at) VALUES
			('l1', 'tv', 'hdmi:m|hdmi:m', '` + shared + `', '` + shared + `', 1, 1)`,
		`INSERT INTO links (id, system_id, part_key, preferred_item, created_at, updated_at) VALUES
			('l2', 'desk', 'hdmi:m|hdmi:m', '` + shared + `', 1, 1),
			('l3', 'desk', 'hdmi:m|hdmi:m', '` + solo + `', 1, 1)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	inv, err := db.LoadInventory(catalog.KindCable)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]library.Status{shared: library.InUse, solo: library.Reserved, spare: library.Free} {
		if got := inv.Status[id]; got != want {
			t.Errorf("status = %s, want %s", got, want)
		}
	}
	if inv.HeldBy[shared] != "TV" {
		t.Errorf("HeldBy = %q, want TV", inv.HeldBy[shared])
	}
	// Taking the TV down frees the shared cable back to reserved.
	db.Exec(`UPDATE systems SET active = 0 WHERE id = 'tv'`)
	inv, _ = db.LoadInventory(catalog.KindCable)
	if got := inv.Status[shared]; got != library.Reserved {
		t.Errorf("after TV down: status = %s, want reserved", got)
	}
}
