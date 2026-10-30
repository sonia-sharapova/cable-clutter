package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
