package catalog_test

import (
	"math/rand"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
)

func load(t testing.TB) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(catalogdata.FS, catalogdata.Version)
	if err != nil {
		t.Fatalf("bundled catalog does not load: %v", err)
	}
	return c
}

func TestBundledCatalogLoads(t *testing.T) {
	c := load(t)
	if len(c.Connectors) < 40 || len(c.Cables) < 50 || len(c.Adapters) < 15 || len(c.Signals) < 15 {
		t.Errorf("catalog smaller than the milestone 1 seed: %d connectors, %d cables, %d adapters, %d signals",
			len(c.Connectors), len(c.Cables), len(c.Adapters), len(c.Signals))
	}
}

func TestKeyIgnoresEndOrder(t *testing.T) {
	c := load(t)
	rng := rand.New(rand.NewSource(1))
	for _, m := range []map[string]*catalog.Part{c.Cables, c.Adapters} {
		for key, p := range m {
			ends := append([]catalog.End(nil), p.Ends...)
			for range 20 {
				rng.Shuffle(len(ends), func(i, j int) { ends[i], ends[j] = ends[j], ends[i] })
				if got := catalog.Key(ends, p.Variant); got != key {
					t.Fatalf("%s: shuffled ends gave key %q, want %q", p.Name, got, key)
				}
			}
		}
	}
}

func TestParseEnd(t *testing.T) {
	for in, want := range map[string]string{
		"usb-c:m":   "usb-c:m",
		" USB-C:M ": "usb-c:m",
		"rca*3:m":   "rca*3:m",
		"rca*1:f":   "rca:f",
	} {
		e, err := catalog.ParseEnd(in)
		if err != nil {
			t.Errorf("ParseEnd(%q): %v", in, err)
			continue
		}
		if e.String() != want {
			t.Errorf("ParseEnd(%q) = %q, want %q", in, e, want)
		}
	}
	for _, bad := range []string{"usb-c", "usb-c:x", ":m", "rca*0:m", "rca*two:m"} {
		if _, err := catalog.ParseEnd(bad); err == nil {
			t.Errorf("ParseEnd(%q) succeeded, want error", bad)
		}
	}
}

func TestFindRanksExactConnectorFirst(t *testing.T) {
	c := load(t)
	for q, want := range map[string]string{
		"aux":       "trs-3.5",
		"Aux":       "trs-3.5",
		"3.5 mm":    "trs-3.5",
		"3.5MM":     "trs-3.5",
		"usb c":     "usb-c",
		"USB-C":     "usb-c",
		"usbs":      "usb-a-3.0",
		"lightning": "lightning",
		"tf":        "microsd",
	} {
		refs := c.Find(q)
		if len(refs) == 0 || refs[0].Kind != catalog.RefConnector || refs[0].ID != want {
			t.Errorf("Find(%q) first result = %v, want connector %q", q, refs, want)
		}
	}
}

func TestFindMatchesWordStarts(t *testing.T) {
	c := load(t)
	for q, wantKey := range map[string]string{
		"lightning": "lightning:m|usb-a:m",
		"splitter":  "trs-3.5*2:f|trs-3.5:m",
		"y cable":   "rca*2:m|trs-3.5:m",
	} {
		found := false
		for _, r := range c.Find(q) {
			found = found || r.ID == wantKey
		}
		if !found {
			t.Errorf("Find(%q) does not include %s", q, wantKey)
		}
	}
	if refs := c.Find("zzz-not-a-cable"); len(refs) != 0 {
		t.Errorf("Find(nonsense) = %v, want nothing", refs)
	}
}

func TestResolveConnector(t *testing.T) {
	c := load(t)
	for in, want := range map[string]string{"aux": "trs-3.5", "usb-c": "usb-c", "Type C": "usb-c", "30 pin": "apple-30pin"} {
		if got, ok := c.ResolveConnector(in); !ok || got != want {
			t.Errorf("ResolveConnector(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := c.ResolveConnector("toaster"); ok {
		t.Error("ResolveConnector(toaster) succeeded")
	}
}

func TestByConnectorIndex(t *testing.T) {
	c := load(t)
	for _, p := range c.ByConnector["ps2-avmulti"] {
		if !strings.Contains(p.Key, "ps2-avmulti") {
			t.Errorf("ByConnector[ps2-avmulti] holds %s", p.Key)
		}
	}
	if len(c.ByConnector["ps2-avmulti"]) != 3 {
		t.Errorf("want 3 PlayStation AV cables, got %d", len(c.ByConnector["ps2-avmulti"]))
	}
}

func TestLoadRejectsBadCatalogs(t *testing.T) {
	base := "signals:\n  - {id: s, kind: data, name: S}\nconnectors:\n  - {id: a, family: x, name: A}\n  - {id: b, family: x, name: B}\n" +
		"glyphs:\n  default: {fill: '=', art: \"[#]\"}\n"
	ok := base + "cables:\n  - {ends: [a:m, b:m], signals: [s], name: X}\n"
	if _, err := catalog.Load(fstest.MapFS{"c.yaml": {Data: []byte(ok)}}, "test"); err != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}
	for name, body := range map[string]string{
		"unknown connector": base + "cables:\n  - {ends: [a:m, nope:m], signals: [s], name: X}\n",
		"unknown signal":    base + "cables:\n  - {ends: [a:m, b:m], signals: [nope], name: X}\n",
		"one end":           base + "cables:\n  - {ends: [a:m], signals: [s], name: X}\n",
		"duplicate key":     base + "cables:\n  - {ends: [a:m, b:m], signals: [s], name: X}\n  - {ends: [b:m, a:m], signals: [s], name: Y}\n",
		"bad gender":        base + "cables:\n  - {ends: [a:x, b:m], signals: [s], name: X}\n",
		"glyph for nothing": strings.Replace(ok, "glyphs:\n", "glyphs:\n  nope: {fill: '=', art: x}\n", 1),
		"wide glyph":        strings.Replace(ok, "glyphs:\n", "glyphs:\n  a: {fill: '=', art: \"123456789012345\"}\n", 1),
		"no default glyph":  strings.Replace(ok, "  default:", "  a:", 1),
	} {
		fsys := fstest.MapFS{"c.yaml": {Data: []byte(body)}}
		if _, err := catalog.Load(fsys, "test"); err == nil {
			t.Errorf("%s: Load succeeded, want error", name)
		}
	}
}

// Milestone 1 exit criterion: a lookup takes under 1 ms.
func TestFindUnderOneMillisecond(t *testing.T) {
	c := load(t)
	const n = 1000
	start := time.Now()
	for range n {
		c.Find("aux")
	}
	if avg := time.Since(start) / n; avg > time.Millisecond {
		t.Errorf("Find averaged %s, budget is 1ms", avg)
	}
}

func BenchmarkFind(b *testing.B) {
	c := load(b)
	for b.Loop() {
		c.Find("aux")
	}
}

func BenchmarkPartLookup(b *testing.B) {
	c := load(b)
	for b.Loop() {
		c.Part("lightning:m|usb-a:m")
	}
}

func BenchmarkLoad(b *testing.B) {
	for b.Loop() {
		load(b)
	}
}
