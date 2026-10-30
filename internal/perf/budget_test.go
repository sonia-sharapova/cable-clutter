// Package perf_test holds the performance budgets from the data plan as
// tests, run against a stress catalog (the bundled one plus 5,000 generated
// cable types) and a library of 500 cables. Each budget is the average over
// many runs, so one slow run on a busy CI machine doesn't fail the build.
package perf_test

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/advise"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/library"
	"github.com/sonia-sharapova/cable-clutter/internal/plan"
	"github.com/sonia-sharapova/cable-clutter/internal/route"
)

const (
	extraTypes   = 5000
	ownedCables  = 500
	budgetLoad   = 30 * time.Millisecond
	budgetLookup = time.Millisecond
	budgetDevice = 10 * time.Millisecond
	budgetWrite  = 5 * time.Millisecond
	budgetRoute  = 20 * time.Millisecond
)

// stressFS is the bundled catalog plus extraTypes generated cable types
// spread over every pair of USB, video and audio connectors.
func stressFS() fstest.MapFS {
	fsys := fstest.MapFS{}
	files, _ := fs.Glob(catalogdata.FS, "*.yaml")
	for _, name := range files {
		data, _ := fs.ReadFile(catalogdata.FS, name)
		fsys[name] = &fstest.MapFile{Data: data}
	}
	conns := []string{"usb-a", "usb-c", "micro-usb", "mini-usb", "lightning", "hdmi", "displayport", "trs-3.5", "rca", "rj45"}
	var b strings.Builder
	b.WriteString("cables:\n")
	for i := range extraTypes {
		a, z := conns[i%len(conns)], conns[(i/len(conns))%len(conns)]
		fmt.Fprintf(&b, "  - {ends: [%s:m, %s:m], variant: gen%d, signals: [usb.2.0], name: \"Generated %d\"}\n", a, z, i, i)
	}
	fsys["zz-generated.yaml"] = &fstest.MapFile{Data: []byte(b.String())}
	return fsys
}

// stressCatalog loads a compiled snapshot, as the CLI does at start-up.
func stressCatalog(t testing.TB, compiled []byte) *catalog.Catalog {
	t.Helper()
	c, err := catalog.LoadCompiled(compiled, "stress")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func compile(t testing.TB, fsys fs.FS) []byte {
	t.Helper()
	data, err := catalog.Compile(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func average(n int, f func()) time.Duration {
	start := time.Now()
	for range n {
		f()
	}
	return time.Since(start) / time.Duration(n)
}

func check(t *testing.T, what string, got, budget time.Duration) {
	t.Helper()
	t.Logf("%-28s %10s  (budget %s)", what, got.Round(time.Microsecond), budget)
	if got > budget {
		t.Errorf("%s took %s on average; budget is %s", what, got, budget)
	}
}

func TestBudgets(t *testing.T) {
	compiled := compile(t, stressFS())
	check(t, "catalog load + indexes", average(10, func() { stressCatalog(t, compiled) }), budgetLoad)
	check(t, "bundled catalog load", average(20, func() { catalog.LoadCompiled(catalogdata.Compiled, catalogdata.Version) }), budgetLoad)

	c := stressCatalog(t, compiled)
	check(t, "find (alias + prefix)", average(1000, func() { c.Find("usb") }), budgetLookup)
	check(t, "part lookup by key", average(10000, func() { c.Part("hdmi:m|hdmi:m") }), budgetLookup)

	db, err := library.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keys := []string{"usb-a:m|usb-c:m", "hdmi:m|hdmi:m", "trs-3.5:m|trs-3.5:m", "rca*3:m|rca*3:m", "micro-usb:m|usb-a:m"}
	for i := range ownedCables {
		if _, err := db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: keys[i%len(keys)]}, 1); err != nil {
			t.Fatal(err)
		}
	}
	check(t, "add a cable (durable)", average(50, func() {
		db.AddItems(library.Item{Kind: catalog.KindCable, PartKey: "hdmi:m|hdmi:m"}, 1)
	}), budgetWrite)

	s, _ := db.NewSystem("Bench", "")
	l, _ := db.AddLink(library.Link{SystemID: s.ID, PartKey: "hdmi:m|hdmi:m"})
	cables, _ := db.LoadInventory(catalog.KindCable)
	item := cables.Owned["hdmi:m|hdmi:m"][0].ID
	check(t, "system up + down (durable)", average(50, func() {
		db.BringUp(s.ID, map[string]string{l.ID: item})
		db.TakeDown(s.ID)
	})/2, budgetWrite)

	var inv plan.Inventories
	check(t, "load inventory (500 cables)", average(50, func() {
		inv.Cables, _ = db.LoadInventory(catalog.KindCable)
		inv.Adapters, _ = db.LoadInventory(catalog.KindAdapter)
	}), budgetDevice)
	mac := c.Devices["mac-mini-m2-2023"]
	check(t, "device report", average(200, func() { advise.Device(c, mac, inv.Cables, inv.Adapters, "") }), budgetDevice)
	check(t, "connect, 3 steps", average(50, func() { route.Connect(c, mac, c.Devices["crt-tv"], inv, route.Options{}) }), budgetRoute)
}

func BenchmarkStressLoad(b *testing.B) {
	compiled := compile(b, stressFS())
	for b.Loop() {
		stressCatalog(b, compiled)
	}
}
