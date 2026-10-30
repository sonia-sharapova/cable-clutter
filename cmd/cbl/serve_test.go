package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/sonia-sharapova/cable-clutter/internal/library"
)

func TestServe(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "add", "ps2-avmulti", "rca*3", "--name", "grey ps2 av")
	mustRun(t, "add", "hdmi", "hdmi")
	mustRun(t, "device", "add", "ps2", "--name", "PS2")
	mustRun(t, "device", "add", "crt-tv", "--name", "CRT")
	mustRun(t, "plan", "ps2", "crt", "--save", "Retro corner")

	srv := httptest.NewServer(newServer())
	defer srv.Close()
	get := func(path string) (int, string) {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	post := func(path, body string, header bool) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		if header {
			req.Header.Set("X-CBL", "1")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v
	}

	for _, path := range []string{"/", "/app.js", "/style.css"} {
		if code, body := get(path); code != 200 || len(body) < 500 {
			t.Errorf("GET %s: %d, %d bytes", path, code, len(body))
		}
	}

	code, body := get("/api/index")
	var index struct {
		Entries []entry        `json:"entries"`
		Counts  map[string]int `json:"counts"`
		Devices []string       `json:"devices"`
	}
	if err := json.Unmarshal([]byte(body), &index); err != nil || code != 200 {
		t.Fatalf("index: %d %v", code, err)
	}
	kinds := map[string]int{}
	for _, e := range index.Entries {
		kinds[e.Kind]++
	}
	if kinds["cable"] != 2 || kinds["device"] != 2 || kinds["system"] != 1 || kinds["type"] < 50 || kinds["connector"] < 40 {
		t.Errorf("index kinds = %v", kinds)
	}
	if index.Counts["cable"] != 2 || len(index.Devices) != 2 {
		t.Errorf("counts = %v, devices = %v", index.Counts, index.Devices)
	}
	if strings.Contains(body, "\n  ") {
		t.Error("API JSON is pretty-printed; it should be compact")
	}

	for path, want := range map[string]string{
		"/api/part?key=" + url.QueryEscape("ps2-avmulti:m|rca*3:m"): "grey ps2 av",
		"/api/connector?id=rca":                               "PlayStation AV cable",
		"/api/device?name=PS2":                                "AV Multi Out",
		"/api/device?name=apple-tv-hd":                        "Apple TV",
		"/api/system?name=" + url.QueryEscape("Retro corner"): "PlayStation AV cable",
		"/api/connect?from=PS2&to=CRT":                        `"video"`,
		"/api/wish":                                           "[",
	} {
		if code, body := get(path); code != 200 || !strings.Contains(body, want) {
			t.Errorf("GET %s: %d, want %q in %.300s", path, code, want, body)
		}
	}
	if code, _ := get("/api/device?name=toaster"); code != 400 {
		t.Errorf("unknown device: %d, want 400", code)
	}

	// Writes need the X-CBL header, which a cross-site form can't send.
	if code, _ := post("/api/system/up", `{"name":"Retro corner"}`, false); code != 403 {
		t.Errorf("up without header: %d, want 403", code)
	}
	if code, v := post("/api/system/up", `{"name":"Retro corner"}`, true); code != 200 || v["up"] != true {
		t.Errorf("up: %d %v", code, v)
	}
	if out := mustRun(t, "ls", "--in-use"); !strings.Contains(out, "grey ps2 av") {
		t.Errorf("up through the page didn't mark the cable in use:\n%s", out)
	}
	if code, v := post("/api/system/down", `{"name":"Retro corner"}`, true); code != 200 || v["up"] != false {
		t.Errorf("down: %d %v", code, v)
	}
	mustRun(t, "wish", "add", "aux", "rca")
	_, body = get("/api/wish")
	var wishes []map[string]any
	json.Unmarshal([]byte(body), &wishes)
	if code, v := post("/api/wish/done", `{"id":"`+wishes[0]["id"].(string)+`"}`, true); code != 200 || v["added"] == nil {
		t.Errorf("wish done: %d %v", code, v)
	}
}

func TestServeConnectAndWishAdd(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "device", "add", "raspberry-pi-3b", "--name", "Pi")
	mustRun(t, "device", "add", "crt-tv", "--name", "CRT")
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/connect?from=Pi&to=CRT")
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		ToBuy    []struct{ Key, Name string } `json:"to_buy"`
		Outlines map[string]string            `json:"outlines"`
	}
	json.NewDecoder(res.Body).Decode(&d)
	res.Body.Close()
	if len(d.ToBuy) == 0 || d.ToBuy[0].Key == "" || d.Outlines[d.ToBuy[0].Key] == "" {
		t.Fatalf("connect: to_buy %v, outlines for %d parts", d.ToBuy, len(d.Outlines))
	}

	add := func() []any {
		req, _ := http.NewRequest("POST", srv.URL+"/api/wish/add", strings.NewReader(`{"parts":["`+d.ToBuy[0].Key+`"],"note":"Pi → CRT"}`))
		req.Header.Set("X-CBL", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v struct{ Added []any }
		json.NewDecoder(res.Body).Decode(&v)
		return v.Added
	}
	if got := add(); len(got) != 1 {
		t.Errorf("first add: %v", got)
	}
	if got := add(); len(got) != 0 {
		t.Errorf("second add should skip the existing wish: %v", got)
	}
}

// The page builds nested child lists; DOM replaceChildren prints those as
// "[object HTMLElement]". Everything must go through fill(), which flattens.
func TestPageRendersThroughFill(t *testing.T) {
	js, err := webFiles.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(js), ".replaceChildren("); n != 1 {
		t.Errorf("app.js calls replaceChildren %d times; only fill() may", n)
	}
}

func TestServeInventoryAndItemAdd(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "add", "aux", "rca", "--where", "drawer 2")
	mustRun(t, "adapter", "add", "sata", "usb-a-3.0")
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	type row struct {
		Kind       string
		Categories []string
		Items      []itemView
	}
	inventory := func() map[string]row {
		res, err := http.Get(srv.URL + "/api/inventory")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var rows []row
		json.NewDecoder(res.Body).Decode(&rows)
		byKind := map[string]row{}
		for _, r := range rows {
			byKind[r.Kind] = r
		}
		return byKind
	}
	rows := inventory()
	if c := rows["cable"]; len(c.Items) != 1 || strings.Join(c.Categories, ",") != "audio" || c.Items[0].Location != "drawer 2" {
		t.Errorf("cable row = %+v", c)
	}
	if a := rows["adapter"]; strings.Join(a.Categories, ",") != "data" {
		t.Errorf("adapter row = %+v", a)
	}

	req, _ := http.NewRequest("POST", srv.URL+"/api/item/add", strings.NewReader(`{"part":"rj45:m|rj45:m"}`))
	req.Header.Set("X-CBL", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("item add: %v %v", err, res.Status)
	}
	res.Body.Close()
	if c := inventory()["cable"]; len(c.Items) != 1 {
		t.Errorf("cable rows changed unexpectedly: %+v", c)
	}
	if out := mustRun(t, "ls"); !strings.Contains(out, "Ethernet cable") {
		t.Errorf("item added through the page isn't in the inventory:\n%s", out)
	}
}

func TestServeSystemBuilder(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "add", "ps2-avmulti", "rca*3")
	mustRun(t, "device", "add", "ps2", "--name", "PS2")
	mustRun(t, "device", "add", "crt-tv", "--name", "CRT")
	mustRun(t, "device", "add", "mac-mini-m2-2023", "--name", "Mac")
	srv := httptest.NewServer(newServer())
	defer srv.Close()
	post := func(path, body string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("X-CBL", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v
	}

	// The builder offers catalog-wide cables per port, flagged uncommon.
	res, _ := http.Get(srv.URL + "/api/builder")
	var b struct {
		Devices []struct {
			Name  string
			Ports []struct {
				Label string
				Fits  []string
			}
		}
		Parts map[string]struct {
			Uncommon bool
			Items    []itemView
		}
	}
	json.NewDecoder(res.Body).Decode(&b)
	res.Body.Close()
	var hdmiFits []string
	for _, d := range b.Devices {
		for _, p := range d.Ports {
			if d.Name == "Mac" && p.Label == "HDMI" {
				hdmiFits = p.Fits
			}
		}
	}
	if !slices.Contains(hdmiFits, "hdmi:m|hdmi:m") || !slices.Contains(hdmiFits, "hdmi:m|mini-hdmi:m") {
		t.Errorf("Mac HDMI fits = %v, want catalog-wide types", hdmiFits)
	}
	if !b.Parts["hdmi:m|mini-hdmi:m"].Uncommon || b.Parts["hdmi:m|hdmi:m"].Uncommon {
		t.Error("uncommon flags wrong")
	}
	if len(b.Parts["ps2-avmulti:m|rca*3:m"].Items) != 1 {
		t.Error("owned items missing from builder")
	}

	// Details hidden: planned, links left open.
	if code, v := post("/api/system/new", `{"name":"Retro","devices":["PS2","CRT"]}`); code != 200 || v["links"] != 1.0 {
		t.Fatalf("auto system: %d %v", code, v)
	}
	if out := mustRun(t, "system", "show", "Retro"); !strings.Contains(out, "PS2 · AV Multi Out → CRT · AV in") || strings.Contains(out, "pinned") {
		t.Errorf("auto system should have an open link:\n%s", out)
	}
	// Details shown: exactly the links given, validated against the ports.
	if code, _ := post("/api/system/new", `{"name":"Bad","links":[{"part":"hdmi:m|hdmi:m","from_device":"PS2","from_port":"AV Multi Out"}]}`); code != 400 {
		t.Errorf("HDMI into AV Multi Out accepted: %d", code)
	}
	if code, v := post("/api/system/new", `{"name":"Desk","links":[{"part":"hdmi:m|hdmi:m","from_device":"Mac","from_port":"HDMI"}]}`); code != 200 || v["links"] != 1.0 {
		t.Fatalf("detailed system: %d %v", code, v)
	}
	code, v := post("/api/system/link", `{"system":"Desk","part":"trs-3.5:m|trs-3.5:m","from_device":"Mac"}`)
	if code != 200 || v["from_port"] != "Headphone" {
		t.Fatalf("add link (port should be filled in): %d %v", code, v)
	}
	if code, _ := post("/api/system/unlink", `{"id":"`+v["id"].(string)+`"}`); code != 200 {
		t.Errorf("unlink: %d", code)
	}
	if code, _ := post("/api/system/delete", `{"name":"Desk"}`); code != 200 {
		t.Errorf("delete: %d", code)
	}
	if out := mustRun(t, "system", "ls"); strings.Contains(out, "Desk") || !strings.Contains(out, "Retro") {
		t.Errorf("after delete:\n%s", out)
	}
	if code, _ := post("/api/system/new", `{"name":"Solo","devices":["PS2"]}`); code != 400 {
		t.Errorf("one device and no links accepted: %d", code)
	}
}

func TestServeWishlistUseCase(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "wish", "add", "aux", "rca")
	mustRun(t, "device", "add", "raspberry-pi-3b", "--name", "Pi")
	mustRun(t, "device", "add", "crt-tv", "--name", "CRT")
	mustRun(t, "connect", "pi", "crt", "--wish")
	mustRun(t, "wish", "done", "1")
	srv := httptest.NewServer(newServer())
	defer srv.Close()
	res, _ := http.Get(srv.URL + "/api/wish")
	var wishes []struct {
		UseCase string `json:"use_case"`
		Done    bool
	}
	json.NewDecoder(res.Body).Decode(&wishes)
	res.Body.Close()
	if len(wishes) != 2 || wishes[0].UseCase != "-" || !wishes[0].Done || wishes[1].UseCase != "Pi → CRT" || wishes[1].Done {
		t.Errorf("wishes = %+v", wishes)
	}
}

func TestServeBookmarks(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "device", "add", "raspberry-pi-3b", "--name", "Pi")
	mustRun(t, "device", "add", "crt-tv", "--name", "CRT")
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	post := func(path, body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("X-CBL", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v
	}
	list := func() []map[string]any {
		res, err := http.Get(srv.URL + "/api/bookmarks")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v []map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return v
	}

	if code, v := post("/api/bookmark/add", `{"name":"Retro","from":"Pi","to":"Nowhere"}`); code != http.StatusBadRequest {
		t.Errorf("unknown device: %d %v", code, v)
	}
	code, added := post("/api/bookmark/add", `{"name":" Retro ","from":"Pi","to":"CRT"}`)
	if code != http.StatusOK || added["name"] != "Retro" {
		t.Fatalf("add: %d %v", code, added)
	}
	if got := list(); len(got) != 1 || got[0]["from"] != "Pi" || got[0]["to"] != "CRT" {
		t.Fatalf("list: %v", got)
	}
	if code, v := post("/api/bookmark/delete", `{"id":"`+added["id"].(string)+`"}`); code != http.StatusOK {
		t.Fatalf("delete: %d %v", code, v)
	}
	if got := list(); len(got) != 0 {
		t.Errorf("after delete: %v", got)
	}
}

func TestServeCustomAdd(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "device", "add", "crt-tv", "--name", "CRT")
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	post := func(body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", srv.URL+"/api/custom/add", strings.NewReader(body))
		req.Header.Set("X-CBL", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v
	}

	if code, v := post(`{"kind":"cable","name":"Odd lead","ends":["usb-a:m"],"signals":["usb.2.0"]}`); code != http.StatusBadRequest {
		t.Errorf("one end: %d %v", code, v)
	}
	code, v := post(`{"kind":"cable","name":"Odd lead","ends":["usb-a:m","rca*3:m"],"variant":"mine","signals":["video.composite"],"qty":2,"location":"drawer"}`)
	if code != http.StatusOK || v["added"] != 2.0 {
		t.Fatalf("custom cable: %d %v", code, v)
	}
	code, v = post(`{"kind":"device","name":"Gizmo","nickname":"Gizmo","ports":[{"label":"AV out","end":"rca*3:f","dir":"out","signals":["video.composite"]}]}`)
	if code != http.StatusOK || v["model"] != "custom-gizmo" {
		t.Fatalf("custom device: %d %v", code, v)
	}

	// Both now live in the catalog: the cable in the inventory, the device in routes.
	res, err := http.Get(srv.URL + "/api/inventory")
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(inv), "Odd lead") {
		t.Errorf("inventory lacks the custom cable: %s", inv)
	}
	res, err = http.Get(srv.URL + "/api/connect?from=Gizmo&to=CRT")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("connect from a custom device: %v %v", err, res.Status)
	}
	res.Body.Close()
}

func TestServeExplore(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "add", "usb-a", "usb-c")
	srv := httptest.NewServer(newServer())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/explore")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var families []struct {
		Family     string
		Connectors []struct {
			ID    string
			Parts []struct {
				Key   string
				Owned int
			}
		}
	}
	json.NewDecoder(res.Body).Decode(&families)
	var names []string
	owned := 0
	for _, f := range families {
		names = append(names, f.Family)
		for _, c := range f.Connectors {
			for _, p := range c.Parts {
				owned += p.Owned
			}
		}
	}
	if !slices.IsSorted(names) || !slices.Contains(names, "usb") {
		t.Errorf("families not sorted or missing usb: %v", names)
	}
	if owned != 2 { // the cable shows under both of its connectors
		t.Errorf("owned count across connectors = %d, want 2", owned)
	}
}

func TestServeSystemDevices(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "device", "add", "mac-mini-m2-2023", "--name", "Mac")
	mustRun(t, "device", "add", "dell-monitor", "--name", "Monitor")
	mustRun(t, "device", "add", "te-ob-4", "--name", "Speaker")
	srv := httptest.NewServer(newServer())
	defer srv.Close()
	post := func(path, body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("X-CBL", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v
	}
	if code, v := post("/api/system/new", `{"name":"Desk","devices":["Mac","Monitor"]}`); code != 200 {
		t.Fatalf("new: %d %v", code, v)
	}
	// The speaker takes the Mac's sound in: found by trying the other way round.
	if code, v := post("/api/system/add-device", `{"system":"Desk","device":"Speaker","to":"Mac"}`); code != 200 {
		t.Fatalf("add speaker: %d %v", code, v)
	}
	if out := mustRun(t, "system", "show", "Desk"); !strings.Contains(out, "Speaker") {
		t.Fatalf("speaker not in the system:\n%s", out)
	}
	if code, v := post("/api/system/remove-device", `{"system":"Desk","device":"Speaker"}`); code != 200 || v["removed"].(float64) < 1 {
		t.Fatalf("remove speaker: %d %v", code, v)
	}
	out := mustRun(t, "system", "show", "Desk")
	if strings.Contains(out, "Speaker") || !strings.Contains(out, "Monitor") {
		t.Errorf("after removing the speaker:\n%s", out)
	}
	if code, _ := post("/api/system/remove-device", `{"system":"Desk","device":"Speaker"}`); code != 400 {
		t.Error("removing a device that isn't there should fail")
	}
}

func TestLinkChains(t *testing.T) {
	l := func(id, from, to string) *library.Link { return &library.Link{ID: id, FromDevice: from, ToDevice: to} }
	got := linkChains([]*library.Link{
		l("1", "Mac", ""), l("2", "", ""), l("3", "", "Monitor"), // a planned route through an adapter
		l("4", "Mac", "Speaker"), // a manual one-cable link
		l("5", "", ""),           // a cable left open at both ends
	})
	var ids []string
	for _, c := range got {
		s := ""
		for _, x := range c {
			s += x.ID
		}
		ids = append(ids, s)
	}
	if strings.Join(ids, ",") != "123,4,5" {
		t.Errorf("chains = %v, want 123,4,5", ids)
	}
}

// A wired keyboard and mouse plug straight into the Pi: no cables to own,
// and the system still comes up.
func TestServeDirectPlugIn(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "device", "add", "raspberry-pi-3b", "--name", "Pi", "--owner", "Alex")
	mustRun(t, "device", "add", "dell-monitor", "--name", "Monitor")
	mustRun(t, "device", "add", "generic-usb-keyboard", "--name", "Keyboard")
	mustRun(t, "add", "hdmi", "hdmi")
	srv := httptest.NewServer(newServer())
	defer srv.Close()
	post := func(path, body string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("X-CBL", "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		return res.StatusCode, v
	}
	// A mouse you don't know the model of: ports from the generic type.
	if code, v := post("/api/custom/add", `{"kind":"device","name":"Old mouse","generic":"generic-usb-mouse","owner":"Alex"}`); code != 200 {
		t.Fatalf("generic mouse: %d %v", code, v)
	}
	if code, v := post("/api/system/new", `{"name":"Pi desk","devices":["Pi","Monitor"]}`); code != 200 {
		t.Fatalf("new: %d %v", code, v)
	}
	for _, dev := range []string{"Keyboard", "Old mouse"} {
		if code, v := post("/api/system/add-device", `{"system":"Pi desk","device":"`+dev+`","to":"Pi"}`); code != 200 {
			t.Fatalf("add %s: %d %v", dev, code, v)
		}
	}
	out := mustRun(t, "system", "show", "Pi desk")
	if strings.Count(out, "plugs straight in") != 2 {
		t.Errorf("want two direct links:\n%s", out)
	}
	if code, v := post("/api/system/up", `{"name":"Pi desk"}`); code != 200 || v["up"] != true {
		t.Errorf("switch on: %d %v", code, v)
	}
	if out := mustRun(t, "device", "ls"); !strings.Contains(out, "Alex") {
		t.Errorf("owner missing from device ls:\n%s", out)
	}
}
