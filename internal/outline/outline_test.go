package outline_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	catalogdata "github.com/sonia-sharapova/cable-clutter/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/catalog"
	"github.com/sonia-sharapova/cable-clutter/internal/outline"
)

var update = flag.Bool("update", false, "rewrite golden files")

func load(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(catalogdata.FS, catalogdata.Version)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Milestone 3 exit criterion: every drawing fits an 80-column terminal.
func TestEveryPartFitsIn80Columns(t *testing.T) {
	c := load(t)
	for _, m := range []map[string]*catalog.Part{c.Cables, c.Adapters} {
		for _, p := range m {
			// cbl show indents drawings by 2 columns.
			for _, line := range strings.Split(outline.Render(c, p, ""), "\n") {
				if n := len(line) + 2; n > 80 {
					t.Errorf("%s: line is %d columns:\n%s", p.Name, n, line)
				}
			}
		}
	}
}

func TestWireTouchesBothPlugs(t *testing.T) {
	c := load(t)
	for _, m := range []map[string]*catalog.Part{c.Cables, c.Adapters} {
		for _, p := range m {
			out := outline.Render(c, p, "")
			for _, gap := range []string{" ~", "~ ", " =+", "+= "} {
				for _, line := range strings.Split(out, "\n") {
					if strings.Contains(strings.TrimSpace(line), gap) {
						t.Errorf("%s: wire stops short (%q):\n%s", p.Name, gap, out)
					}
				}
			}
		}
	}
}

func TestColorWrapsOutlinesOnly(t *testing.T) {
	c := load(t)
	p, _ := c.Part("usb-a:m|usb-c:m")
	out := outline.Render(c, p, outline.Green)
	label := strings.Split(out, "\n")[0]
	if strings.Contains(label, "\x1b") {
		t.Errorf("label line is coloured: %q", label)
	}
	if !strings.Contains(out, outline.Green) || !strings.Contains(out, outline.Reset) {
		t.Error("outlines are not coloured")
	}
}

func TestGolden(t *testing.T) {
	c := load(t)
	for name, key := range map[string]string{
		"usb-a-to-usb-c":   "usb-a:m|usb-c:m",
		"ps2-av":           "ps2-avmulti:m|rca*3:m",
		"aux-to-rca":       "rca*2:m|trs-3.5:m",
		"splitter":         "trs-3.5*2:f|trs-3.5:m",
		"usb-c-hub":        "hdmi:f|usb-a:f|usb-c:f|usb-c:m",
		"us-to-eu-adapter": "cee-7-16:m|nema-1-15:f",
	} {
		p, ok := c.Part(key)
		if !ok {
			t.Fatalf("no part %s", key)
		}
		got := outline.Render(c, p, "")
		path := filepath.Join("testdata", name+".txt")
		if *update {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test ./internal/outline -update)", err)
		}
		if got != string(want) {
			t.Errorf("%s changed:\n--- got\n%s--- want\n%s", name, got, want)
		}
	}
}
