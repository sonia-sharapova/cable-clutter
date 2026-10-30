package library_test

import (
	"testing"

	"cable-clutter/internal/catalog"
	"cable-clutter/internal/library"
)

func TestAddDeviceFillsPlaceholder(t *testing.T) {
	db := open(t)
	// `cbl add --for PS2` makes a placeholder device with no model.
	if _, err := db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "ps2-avmulti:m|rca*3:m", Dedicated: "PS2"}, 1); err != nil {
		t.Fatal(err)
	}
	d, created, err := db.AddDevice("ps2-slim", "ps2", "")
	if err != nil || created {
		t.Fatalf("AddDevice: created=%v err=%v, want the placeholder filled", created, err)
	}
	devices, _ := db.Devices()
	if len(devices) != 1 || devices[0].Model != "ps2-slim" || devices[0].ID != d.ID {
		t.Fatalf("devices = %+v", devices[0])
	}
	if _, _, err := db.AddDevice("psp", "PS2", ""); err == nil {
		t.Error("a second device named PS2 was accepted")
	}
	if _, created, err := db.AddDevice("psp", "PSP", ""); err != nil || !created {
		t.Errorf("new device: created=%v err=%v", created, err)
	}
	devices, _ = db.Devices()
	if got := library.FindDevices(devices, "psp"); len(got) != 1 {
		t.Errorf("FindDevices(psp) = %d", len(got))
	}
}

func TestRemoveDeviceKeepsItsCables(t *testing.T) {
	db := open(t)
	db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "ps2-avmulti:m|rca*3:m", Dedicated: "PS2"}, 1)
	devices, _ := db.Devices()
	if err := db.RemoveDevice(devices[0].ID); err != nil {
		t.Fatal(err)
	}
	if devices, _ := db.Devices(); len(devices) != 0 {
		t.Errorf("%d devices left", len(devices))
	}
	inv, _ := db.LoadInventory(catalog.KindCable)
	if len(inv.Order) != 1 || inv.Order[0].Dedicated != "" {
		t.Errorf("cable after device removal: %+v", inv.Order)
	}
}
